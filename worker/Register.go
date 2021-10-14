package worker

import (
	"context"
	"crontab/common"
	"fmt"
	"net"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

//注册节点到etcd: /cron/worker/ip地址
type Register struct {
	client  *clientv3.Client
	kv      clientv3.KV
	lease   clientv3.Lease
	localIP string //本机ip

}

//实现注册的方法 注册到/cron/workers/ip，并自动续租
func (register *Register) keepOnline() {
	var (
		regKey         string
		leaseGrantResp *clientv3.LeaseGrantResponse
		err            error
		keepAliveChan  <-chan *clientv3.LeaseKeepAliveResponse
		keepAliveResp  *clientv3.LeaseKeepAliveResponse
		cancelCtx      context.Context
		cancelFunc     context.CancelFunc
	)
	for {
		//注册路径
		regKey = common.JOB_WORKER_DIR + register.localIP
		cancelFunc = nil
		//创建租约 节点down机10s后这个租约就会过期key也会被删掉, 如果创建租约异常就要重试
		if leaseGrantResp, err = register.client.Grant(context.TODO(), 10); err != nil {
			goto RETRY
		}
		//上边创建租约后，自动续租,在节点存活期间自动续租，防止key失效
		if keepAliveChan, err = register.lease.KeepAlive(context.TODO(), leaseGrantResp.ID); err != nil {
			goto RETRY
		}
		cancelCtx, cancelFunc = context.WithCancel(context.TODO())
		//注册到etcd put如果异常就需要取消自动续租
		if _, err = register.kv.Put(cancelCtx, regKey, "", clientv3.WithLease(leaseGrantResp.ID)); err != nil {
			goto RETRY
		}
		fmt.Println("服务注册etcd成功！")
		//处理续租应答
		for { //如果上边都正常，那么就会夯在这儿，结果就是worker只会注册一次
			select {
			case keepAliveResp = <-keepAliveChan:
				if keepAliveResp == nil { //续租失败,比如节点和etcd之间的网络不通,等到连接上了租约过期了,这种情况就会续租失败.那么就创建新的租约重试.
					goto RETRY
				}
			}
		}

	RETRY:
		time.Sleep(1 * time.Second)
		if cancelFunc != nil {
			cancelFunc()
		}

	}
}

var (
	G_register *Register
)

func getLocalIP() (ipv4 string, err error) {
	var (
		addrs   []net.Addr
		addr    net.Addr
		ipNet   *net.IPNet
		isIpNet bool
	)
	//获取所有的网卡
	if addrs, err = net.InterfaceAddrs(); err != nil {
		return
	}
	//获取第一个非lo的网卡ip
	for _, addr = range addrs {
		//ipv4, ipv6  也可能是unix socket地址，所以下边用了反解，这里的反解可能就是马哥说的反射
		//这个网络地址是ip地址
		if ipNet, isIpNet = addr.(*net.IPNet); isIpNet && !ipNet.IP.IsLoopback() { //如果成功说明的确是一个ip地址,ip地址可能是ipv4和ipv6,并且这个ip地址不是环回地址
			//跳过ipv6
			//如果转化的时候不为空那么就是一个ipv4地址
			if ipNet.IP.To4() != nil {
				ipv4 = ipNet.IP.String() //192.168.1.23
				fmt.Println("当前worker注册的ip:", ipv4)
				return
			}
		}
	}

	err = common.ERR_NOLOCAL_IP_FOUND
	return

}

func InitRegister() (err error) {
	var (
		localIp string
	)

	//初始化配置
	config := clientv3.Config{
		Endpoints:   G_config.EtcdEndpoints,                                     //集群地址
		DialTimeout: time.Duration(G_config.EtcdDialTimeout) * time.Millisecond, //连接超时
	}
	//建立连接
	client, err := clientv3.New(config)
	if err != nil {
		return
	}
	//得到kv和lease的api子集
	kv := clientv3.NewKV(client)
	lease := clientv3.NewLease(client)

	//本机ip
	if localIp, err = getLocalIP(); err != nil {
		return
	}
	G_register = &Register{
		client:  client,
		kv:      kv,
		lease:   lease,
		localIP: localIp,
	}

	//服务注册
	go G_register.keepOnline()
	return
}
