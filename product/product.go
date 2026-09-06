package product

type Product struct {
	ID    int
	Name  string
	Price float64
	Stock int
}

func (p *Product) Sell(quantity int) {
	if quantity <= 0 {
		return
	}

	if quantity > p.Stock {
		return
	}

	p.Stock -= quantity
}

func (p Product) TotalPrice() float64 {
	return p.Price * float64(p.Stock)
}