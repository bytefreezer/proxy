package repos

import (
	"errors"
	"time"

	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/config"
)

var (
	ErrServerResponse = errors.New("error status code from the service")
	ErrDnameResponse  = errors.New("invalid dname")
)

const (
	DEFAULT_TIMEOUT = 30 * time.Second
	DEFAULT_TXT     = "json"
)

type Repositories struct {
	Repo Repository
}

type Repository interface {
}

type DataStore struct {
}

func NewRepositories(c *config.Config) (*Repositories, error) {

	return &Repositories{
		Repo: DataStore{},
	}, nil
}
