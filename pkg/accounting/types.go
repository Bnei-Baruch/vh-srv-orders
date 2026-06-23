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

// EuropeContributionEntry is the per-email result from the Europe batch endpoint.
// Found is false when the email did not match any customer in the European system.
// Contribution amounts have refunds subtracted upstream and may be negative.
type EuropeContributionEntry struct {
	IdentifierType string             `json:"identifierType"`
	Identifier     string             `json:"identifier"`
	Found          bool               `json:"found"`
	Contributions  map[string]float64 `json:"contributions"`
}

// EuropeContributionsResult is the payload of POST /v1/europe/contributions/batch.
type EuropeContributionsResult struct {
	CutoffDate     string                    `json:"cutoffDate"`
	LookbackMonths int                       `json:"lookbackMonths"`
	Results        []EuropeContributionEntry `json:"results"`
}

// europeContributionsRequest deliberately omits lookback_months so the server
// default (12 months) applies, matching the other contribution sources.
type europeContributionsRequest struct {
	Emails []string `json:"emails"`
}

type europeContributionsResponse struct {
	Message string                     `json:"message"`
	Data    *EuropeContributionsResult `json:"data"`
	Success bool                       `json:"success"`
}
