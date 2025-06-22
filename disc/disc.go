package disc

import (
	"fmt"
	"os"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/naming/resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/goflower-io/golib/net/rpc/pjson"
)

var clientFn = sync.OnceValue[*clientv3.Client](initEc)

func GetEc() *clientv3.Client {
	return clientFn()
}

func initEc() *clientv3.Client {
	addr := os.Getenv("ETCD_ADDRESS")
	if addr == "" {
		addr = "http://localhost:2379"
	}
	c, err := clientv3.NewFromURL(addr)
	if err != nil {
		panic(err)
	}
	return c
}

func NewConn(serviceID string) (*grpc.ClientConn, error) {
	resolver, err := resolver.NewBuilder(GetEc())
	if err != nil {
		return nil, err
	}
	return grpc.Dial(fmt.Sprintf("etcd:///%s/node", serviceID),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(resolver),
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype(pjson.Name)),
	)
}
