package storage

// AttributeCount is how many of a Project's Logs carried an Attribute with
// values of one type, over some days. Type is "string", "number" or "bool",
// as the key counts table names it.
type AttributeCount struct {
	Name string        `json:"name"`
	Type AttributeType `json:"type"`
	Logs int64         `json:"logs"`
}
