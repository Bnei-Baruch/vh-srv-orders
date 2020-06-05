package main

func countsOrders() int64 {
	var result int64
	DB.Model(&Order{}).Count(&result)
	return result
}

func generateOrders() Order {
	o := Order{
		Type:         "recurring",
		ProductType:  "galaxy",
		RecuringFreq: 30,
		AccountID:    1,
		Organization: "bb",
		Amount:       20,
		Currency:     "us",
	}

	return o
}

func generatePayment(o Order) Payment {
	p := Payment{
		Amount:  20,
		Type:    "plop",
		OrderID: o.ID,
	}
	return p
}

func generateInvoice(p Payment) Invoice {
	i := Invoice{
		Firstname: "Paul",
		Lastname:  "Jenkins",
		Email:     "Paull.Jenkings@gmail.com",
		Phone:     "+332983945",
		Street:    "Main Street, 145",
		City:      "London",
		State:     "England",
		Postcode:  "W38 7EC",
		Country:   "UK",

		PreferedLanguage: "EN",
		PaymentID:        p.ID,
	}
	return i
}
