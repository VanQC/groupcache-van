# Geecachee_WQC

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