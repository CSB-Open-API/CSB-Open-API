package csb

// Subject is the engage identifier of a subject.
type Subject struct {
	ID         int    `json:"id"`
	EngageCode string `json:"engage_code"`
	Name       string `json:"name"`
}
