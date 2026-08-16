package embed

import "context"

type Embedder interface {
	Embed(context.Context, []string) ([][]float32, error)
	Ping(context.Context) error
	Dimension() int
	Model() string
}
