package dcpool

import (
	"context"
	"fmt"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"go.uber.org/multierr"
	"golang.org/x/sync/errgroup"
	"sync"
)

var dcs = []int{1, 2, 3, 4, 5}

type Pool interface {
	Client(dc int) *tg.Client
	Invoker(dc int) telegram.CloseInvoker
	Default() int
	Close() error
}

type pool struct {
	invokers map[int]telegram.CloseInvoker
	_default int
}

func NewPool(ctx context.Context, c *telegram.Client, size int64) (Pool, error) {
	m := make(map[int]telegram.CloseInvoker)
	mu := &sync.Mutex{}
	curDC := c.Config().ThisDC

	wg, errctx := errgroup.WithContext(ctx)

	for _, dc := range dcs {
		dc := dc
		wg.Go(func() error {
			var (
				invoker telegram.CloseInvoker
				err     error
			)

			if dc == curDC { // can't transfer dc to current dc
				invoker, err = c.Pool(size)
			} else {
				invoker, err = c.DC(errctx, dc, size)
			}

			if err != nil {
				return err
			}

			mu.Lock()
			m[dc] = invoker
			mu.Unlock()

			return nil
		})
	}

	if err := wg.Wait(); err != nil {
		return nil, err
	}

	if _, ok := m[curDC]; !ok {
		return nil, fmt.Errorf("default DC %d not in dcs", curDC)
	}

	return &pool{
		invokers: m,
		_default: curDC,
	}, nil
}

func (p *pool) Client(dc int) *tg.Client {
	return tg.NewClient(p.Invoker(dc))
}

func (p *pool) Invoker(dc int) telegram.CloseInvoker {
	i, ok := p.invokers[dc]
	if !ok {
		return p.invokers[p._default]
	}
	return i
}

func (p *pool) Default() int {
	return p._default
}

func (p *pool) Close() error {
	var err error
	for _, invokers := range p.invokers {
		err = multierr.Append(err, invokers.Close())
	}

	return err
}
