package httpwire

const FeatureErrorList = "error_list"

// Capabilities keeps the base command contract separate from additive features.
type Capabilities struct {
	CommandAPI int      `json:"command_api"`
	Features   []string `json:"features,omitempty"`
}

type ErrorPage struct {
	Errors  []Error `json:"errors"`
	HasMore bool    `json:"has_more"`
}
