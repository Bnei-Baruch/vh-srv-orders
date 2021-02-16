package main

func getProductByID(id int64) int64 {
	return id
}

func initProductConvention() Product {

	// ENGLISH
	productHeaderENG := Description{
		Title:    "Convention Ticket",
		Subtitle: "February 2021",
		Body:     "Discovering Life in the Ten - World Kabbalah Convention 2021 - 25-28 February 2021 - Arvut Hall, Virtual Home"}

	productDescriptionENG := Description{
		Title: "Description",
		Body:  "Participation in the convention"}

	conventionDescriptionENG := ProductDescription{
		Locale:     "en",
		Header:     productHeaderENG,
		Body:       productDescriptionENG,
		TosURL:     "https://kli.one/tos",
		CancelText: "Cancel",
		CancelURL:  "https://convention.kli.one",
		ButtonText: "Buy",
	}

	// SPANISH
	productHeaderSPA := Description{
		Title:    "Boleto de la convención",
		Subtitle: "Febrero 2021",
		Body:     "Discovering Life in the Ten - World Kabbalah Convention 2021 - 25-28 February 2021 - Arvut Hall, Virtual Home"}

	productDescriptionSPA := Description{
		Title: "Description",
		Body:  "Participation in the convention"}

	conventionDescriptionSPA := ProductDescription{
		Locale:     "es",
		Header:     productHeaderSPA,
		Body:       productDescriptionSPA,
		TosURL:     "https://kli.one/tos",
		CancelText: "Cancel",
		CancelURL:  "https://convention.kli.one",
		ButtonText: "Buy",
	}

	// Russian
	productHeaderRUS := Description{
		Title:    "Билет на Конгресс",
		Subtitle: "Febrero 2021",
		Body:     "Discovering Life in the Ten - World Kabbalah Convention 2021 - 25-28 February 2021 - Arvut Hall, Virtual Home"}

	productDescriptionRUS := Description{
		Title: "Description",
		Body:  "Participation in the convention"}

	conventionDescriptionRUS := ProductDescription{
		Locale:     "ru",
		Header:     productHeaderRUS,
		Body:       productDescriptionRUS,
		TosURL:     "https://kli.one/tos",
		CancelText: "Cancel",
		CancelURL:  "https://convention.kli.one",
		ButtonText: "Buy",
	}

	// Hebrew
	productHeaderHEB := Description{
		Title:    "כרטיס כנס",
		Subtitle: "Febrero 2021",
		Body:     "Discovering Life in the Ten - World Kabbalah Convention 2021 - 25-28 February 2021 - Arvut Hall, Virtual Home"}

	productDescriptionHEB := Description{
		Title: "Description",
		Body:  "Participation in the convention"}

	conventionDescriptionHEB := ProductDescription{
		Locale:     "he",
		Header:     productHeaderHEB,
		Body:       productDescriptionHEB,
		TosURL:     "https://kli.one/tos",
		CancelText: "Cancel",
		CancelURL:  "https://convention.kli.one",
		ButtonText: "Buy",
	}

	// Currencies
	conventionPriceDollar := Price{
		Currency: "USD",
		Fixed:    true,
		Amount:   30,
		Min:      30,
		Max:      30,
		Step:     0}

	conventionPriceEuro := Price{
		Currency: "EUR",
		Fixed:    true,
		Amount:   25,
		Min:      25,
		Max:      25,
		Step:     0}

	conventionPriceShekel := Price{
		Currency: "NIS",
		Fixed:    true,
		Amount:   25,
		Min:      25,
		Max:      25,
		Step:     0}

	conventionTicket := Product{
		Descriptions:  []ProductDescription{conventionDescriptionENG, conventionDescriptionHEB, conventionDescriptionRUS, conventionDescriptionSPA},
		Cost:          []Price{conventionPriceDollar, conventionPriceShekel, conventionPriceEuro},
		Type:          "regular",
		ProductType:   "feb2521ticket",
		SKU:           "40033",
		RecurringFreq: 0,
		Installements: 1,
		Organization:  "ben2",
	}

	return conventionTicket

}
