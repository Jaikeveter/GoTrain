package main

import "fmt"

type Product struct {
	ID    int
	Name  string
	Price float64
	Stock int
}

func (p *Product) Sell(quantity int) {
	if quantity > p.Stock {
		fmt.Println("Товаров не достаточно")
	} else if quantity <= 0 {
		fmt.Println("Товаров не может быть меньше или равно 0")
	} else {
		p.Stock -= quantity
	}
}

func (p Product) TotalPrice() float64 {
	return p.Price * float64(p.Stock)
}

func main() {
	product := Product{
		ID:    1,
		Name:  "MacBook",
		Price: 1500,
		Stock: 5,
	}

	fmt.Print(product)

	product.Sell(2)

	fmt.Println(product)
}
