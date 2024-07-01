package benchmarkorchestrator

type PolicyDocument struct {
	Version   string
	Statement []StatementEntry
}

type StatementEntry struct {
	Effect    string
	Action    []string
	Principal map[string][]string `json:",omitempty"`
	Resource  []string            `json:",omitempty"`
}
