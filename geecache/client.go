package geecache

import (
	"context"
	"fmt"
	pb "geecache/geecachepb"
	"geecache/registry"
	clientv3 "go.etcd.io/etcd/client/v3"
	"sync"
	"time"
)

// client 只有一个字段——对等节点的地址，同时实现了 ProtoGetter 接口
type client struct {
	name       string // 格式：groupcache/127.0.0.1:8001
	grpcClient pb.GroupCacheClient
	clientOnce sync.Once
}

var (
	defaultEtcdConfig = clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	}
)

func (c *client) initGrpcClient() {
	cli, err := clientv3.New(defaultEtcdConfig)
	if err != nil {
		panic("create etcd client failed: " + err.Error())
	}
	defer cli.Close() // 关闭 cli 释放资源，且不影响 gRPC服务

	// 测试是否能从etcd中Get到key
	//resp, err := cli.Get(context.Background(), c.name)
	//if err != nil {
	//	fmt.Printf("get from etcd failed, err:%v\n", err)
	//}
	//for _, ev := range resp.Kvs {
	//	fmt.Printf("%s---%s\n", ev.Key, ev.Value)
	//}

	conn, err := registry.EtcdDial(cli, c.name)
	if err != nil {
		panic("etcd dial failed: " + err.Error())
	}
	c.grpcClient = pb.NewGroupCacheClient(conn)
}

// Get 方法，实现 ProtoGetter 接口
func (c *client) Get(in *pb.Request, out *pb.Response) (err error) {
	c.clientOnce.Do(c.initGrpcClient)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	out, err = c.grpcClient.Get(ctx, in)
	if err != nil {
		return fmt.Errorf("grpc client Get() error: %v", err)
	}
	return nil
}

func (c *client) Set(in *pb.SetRequest) (err error) {
	c.clientOnce.Do(c.initGrpcClient)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	_, err = c.grpcClient.Put(ctx, in)
	if err != nil {
		return fmt.Errorf("grpc client Put() error: %v", err)
	}
	return nil
}

func (c *client) Remove(in *pb.Request) (err error) {
	c.clientOnce.Do(c.initGrpcClient)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	_, err = c.grpcClient.Delete(ctx, in)
	if err != nil {
		return fmt.Errorf("grpc client Delete error: %v", err)
	}
	return nil
}
