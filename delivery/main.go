package main

import (
	"log"

	"github.com/google/uuid"
)

type Order struct {
	ID          uuid.UUID
	Name        string
	FoodOrdered string
	Price       int
}

func main() {
	lp := ":9999"

	db, err := NewStorage()
	if err != nil {
		log.Fatalln(err)
	}
	s, err := NewAPIServer(lp, db)
	if err != nil {
		log.Fatalln(err)
	}

	s.Start()
	select {}
}
