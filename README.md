# Groupcache +

## v1.0.0
这一版是在极客兔兔的geecache基础上，根据 groupCache 项目源码改写而成。

相较于 groupCache 项目源码做了适当简化：
+ 忽略了源项目中的 Sink 接口，没有进行池化管理；
+ 忽略了源项目中关于缓存命中、远程访问、本地访问等等次数的统计；

相较于极客兔兔上的geecache项目增加了许多细节，比如：
+ 本地缓存远程节点数据-hotcache
+ group 查询逻辑更加明确
+ 在 httpClient.Get() 引入缓冲池 bufferPool，从而减少高并发情况下内存分配和垃圾回收的开销。

## v1.0.1
这版在 v1.0.0 的基础上，参照 https://github.com/mailgun/groupcache 项目，新增缓存数据更新、删除的功能，增加缓存TTL机制
+ byteView 字段新增过期时间，
+ 在 lru.go 中，查询缓存时检查是否过期，若过期则清除。
+ 提供设置缓存、移除缓存的方法调用。
+ 在删除缓存时，除了删除key所在节点的mainCache，还要删除其余所有节点中的hotCache

## v1.0.2
这版在 v1.0.1 的基础上进行了较大改动，将节点间的服务由基于 http 协议，改为基于 grpc 协议，并使用 etcd 进行服务注册。

### 具体改动：
+ 新增 register.go 用于向 etcd 进行服务注册；
+ server.go 文件实现了原 http.go 中的功能，将服务由 http 更改为基于 grpc；
+ 在 .proto 文件中新增 Put 和 Delete 服务；
+ 将节点 client 从 server.go 中独立到 client.go，方便阅读；
+ 删除 sink.go文件，因为本项目为简化没有使用池化技术

### TODO
+ 项目中增加了过期策略，但是未实现过期立刻删除的功能，只要被查询时才会判断是否过期，从而进行删除

### 项目启动&测试
main.go 中定义了一个简单的api访问，便于进行查询测试
在三个命令行窗口执行以下命令，启动三个节点进行服务
```cmd
    go run main.go -api=1
    go run main.go -port=8002
    go run main.go -port=8003
    
    # 测试样例：
    http://localhost:9999/get?key=tom
    http://localhost:9999/set
    http://localhost:9999/remove?key=tom
    ......
    （可自行进行相关查询、新增、删除）
```
