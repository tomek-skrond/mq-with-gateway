package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	amqp "github.com/rabbitmq/amqp091-go"
)

type APIServer struct {
	DB         *Storage
	mqsession  *MQSession
	listenPort string
}

func NewAPIServer(lp string, db *Storage) (*APIServer, error) {
	session, err := NewMQSession()
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return &APIServer{
		DB:         db,
		mqsession:  session,
		listenPort: lp,
	}, nil
}

// func (s *APIServer) handleAddPayment(w http.ResponseWriter, r *http.Request) {
// 	var paymentReq *PaymentRequest
// 	err := json.NewDecoder(r.Body).Decode(&paymentReq)
// 	if err != nil {
// 		log.Fatalln(err)
// 		w.WriteHeader(http.StatusInternalServerError)
// 		w.Write([]byte("Internal server error"))
// 		return
// 	}
// 	w.WriteHeader(http.StatusOK)
// 	w.Write([]byte(""))
// }

func (s *APIServer) insertMessagesIntoDB(msg []byte) error {
	var newPaymentReq PaymentRequest
	if err := json.Unmarshal(msg, &newPaymentReq); err != nil {
		return err
	}

	if err := s.DB.CreatePaymentRequest(&newPaymentReq); err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func (s *APIServer) checkForNewMessages() {
	// messages := make(chan []byte)

	// q, err := s.mqsession.channel.QueueDeclare(
	// 	"order_payment_queue",
	// 	true,
	// 	false,
	// 	false,
	// 	false,
	// 	nil,
	// )
	// if err != nil {
	// 	log.Println(err)
	// }
	xchange := "orders"
	if err := s.mqsession.channel.ExchangeDeclare(
		xchange,
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		log.Println(err)
	}
	fmt.Println("consuming from exchange:", xchange)

	q, err := s.mqsession.channel.QueueDeclare(
		"",
		false,
		false,
		true,
		false,
		nil,
	)
	if err != nil {
		log.Println(err)
	}

	if err := s.mqsession.channel.QueueBind(
		q.Name,  // queue name
		"",      // routing key
		xchange, // exchange
		false,
		nil,
	); err != nil {
		log.Println(err)

	}

	deliveries, err := s.mqsession.channel.Consume(
		q.Name,
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Println(err)
	}

	var forever chan struct{}

	go func() {
		for msg := range deliveries {
			log.Println("received msg:", func(b []byte) PaymentRequest {
				var pr PaymentRequest
				_ = json.Unmarshal(b, &pr)
				return pr
			}(msg.Body))

			msg := msg.Body
			if err := s.insertMessagesIntoDB(msg); err != nil {
				log.Println(err)
			}
		}
	}()

	log.Println("[*] Waiting for messages.")
	<-forever

}

func (s *APIServer) monitorForSuccessfulPayments() {
	// var forever chan struct{}
	go func() {
		for {
			payments, err := s.DB.GetPayments()
			if err != nil {
				log.Println(err)
			}
			for _, p := range payments {
				if p.Status == "paid" && !p.SentToDelivery {
					log.Println("pass paid order to delivery", p)
					p.SentToDelivery = true
					s.DB.UpdatePaymentByID(p)

					s.publishSuccessfulPayment(p)
				}
			}
			time.Sleep(5 * time.Second)
		}

	}()

	// <-forever
}

func (s *APIServer) publishSuccessfulPayment(p *PaymentRequest) error {

	jsonPayment, err := json.Marshal(p)
	if err != nil {
		return err
	}

	// q, err := s.mqsession.channel.QueueDeclare(
	// 	"payment_delivery_queue",
	// 	true,
	// 	false,
	// 	false,
	// 	false,
	// 	nil,
	// )
	// if err != nil {
	// 	log.Println(err)
	// }
	xchange := "payments"
	err = s.mqsession.channel.ExchangeDeclare(
		xchange,  // name
		"fanout", // type
		true,     // durable
		false,    // auto-deleted
		false,    // internal
		false,    // no-wait
		nil,      // arguments
	)
	fmt.Println("Publishing to exchange:", xchange)

	if err := s.mqsession.channel.Publish(
		xchange,
		"",
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         jsonPayment,
		},
	); err != nil {
		log.Println(err)
	}
	return nil
}

func (s *APIServer) Start() {
	go s.checkForNewMessages()
	go s.monitorForSuccessfulPayments()

	r := mux.NewRouter()
	r.HandleFunc("/pay", s.handleProcessPayment).Methods("POST")
	log.Fatalln(http.ListenAndServe(s.listenPort, r))
	select {}
}

func (s *APIServer) handleProcessPayment(w http.ResponseWriter, r *http.Request) {
	// decode payment from user
	var paymentComing *PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&paymentComing); err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal server error"))
		return
	}

	isPaid, err := s.isAlreadyPaid(paymentComing)
	if err != nil {
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal server error"))
		return
	}

	if !isPaid {
		realPrice, err := s.checkPrice(paymentComing)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal server error"))
			return
		}
		if paymentComing.Price >= realPrice {
			if err := s.DB.updatePaymentStatus(paymentComing, "paid"); err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Internal server error"))
				return
			}

			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
			return
		} else {
			s.DB.updatePaymentStatus(paymentComing, "pending")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("not enough money"))
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("order is already paid for"))
}

func (s *APIServer) isAlreadyPaid(payment *PaymentRequest) (bool, error) {
	payments, err := s.DB.GetPayments()
	if err != nil {
		return false, err
	}

	for _, p := range payments {
		if payment.ID == p.ID {
			if p.Status == "paid" {
				return true, nil
			}
			if p.Status == "pending" {
				return false, nil
			}
			if p.Status == "" {
				// s.DB.updatePaymentStatus(p, "pending")
				return false, nil
			}
			return false, fmt.Errorf("invalid status")
		}
	}
	return false, fmt.Errorf("payment not found")
}
func (s *APIServer) checkPrice(payment *PaymentRequest) (int, error) {
	payments, err := s.DB.GetPayments()
	if err != nil {
		return -1, err
	}
	for _, p := range payments {
		if p.ID == payment.ID {
			//returns real price for order
			return p.Price, nil
		}
	}
	return -1, fmt.Errorf("object not found")
}
