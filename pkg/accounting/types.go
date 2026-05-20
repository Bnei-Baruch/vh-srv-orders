package accounting

type APIError struct {
	Error string `json:"error"`
}

// ContributionsResult is the breakdown returned by GetLastContributions.
// Found is false when the email did not match any customer in any enabled company.
type ContributionsResult struct {
	Found     bool                   `json:"found"`
	Total     map[string]float64     `json:"total"`
	Companies []CompanyContributions `json:"companies"`
}

// CompanyContributions is the per-company breakdown. Found is per this company.
type CompanyContributions struct {
	CompanyID     string             `json:"companyId"`
	CompanyName   string             `json:"companyName"`
	Found         bool               `json:"found"`
	Contributions map[string]float64 `json:"contributions"`
}

type contributionsResponse struct {
	Message string               `json:"message"`
	Data    *ContributionsResult `json:"data"`
	Success bool                 `json:"success"`
}
