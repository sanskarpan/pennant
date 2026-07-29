package model

type Context struct {
	Kind       string         `json:"kind"`
	Key        string         `json:"key"`
	Anonymous  bool           `json:"anonymous"`
	Attributes map[string]any `json:"attributes"`
	Private    []string       `json:"private"`
}

func (c *Context) GetAttribute(name string) (any, bool) {
	switch name {
	case "key":
		return c.Key, true
	case "kind":
		return c.Kind, true
	case "anonymous":
		return c.Anonymous, true
	}
	if c.Attributes == nil {
		return nil, false
	}
	v, ok := c.Attributes[name]
	return v, ok
}
