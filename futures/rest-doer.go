/*
FILE: futures/rest-doer.go

DESCRIPTION:
Minimal REST transport contract used by the futures domain clients. Extracted
into an interface (instead of depending on *rest.Client directly) as a test
seam: contract tests substitute a mock transport without spinning HTTP.
*/

package futures

import (
	"context"

	"github.com/tonymontanov/go-aster/internal/rest"
)

// restDoer — minimal REST transport contract.
type restDoer interface {
	Do(ctx context.Context, opts rest.Options) (rest.Response, map[string]string, error)
}
