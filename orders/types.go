package main

import uuid "github.com/google/uuid"

type MealResponse struct {
	Meals []struct {
		StrMeal string `json:"strMeal"`
	} `json:"meals"`
}

type Order struct {
	ID          uuid.UUID `gorm:"primary_key"`
	Name        string    `json:"name"`
	FoodOrdered string    `json:"food_ordered"`
	Price       int       `json:"price"`
}

func NewOrder(name string, food string) (Order, error) {
	return Order{
		ID:          uuid.New(),
		Name:        name,
		FoodOrdered: food,
		Price:       fetchPrice(),
	}, nil
}
