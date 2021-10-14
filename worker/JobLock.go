package worker

import (
	"context"
	"crontab/common"

	clientv3 "go.etcd.io/etcd/client/v3"
)

//分布式锁(TXN事务)
type JobLock struct {
	//etcd客户端
	kv    clientv3.KV
	lease clientv3.Lease

	jobName    string             //任务名 不同的任务上不同的锁，不是全局锁，不同的任务可以并发执行，同一个任务不能并发执行
	cancelFunc context.CancelFunc //用于终止自动续租
	leaseId    clientv3.LeaseID   //租约id
	isLocked   bool               //是否上锁成功
}

//尝试上锁方法，也就是说抢得到锁就抢到了，抢不到就算了
func (jobLock *JobLock) TryLock() (err error) {
	var (
		leaseGrantResp *clientv3.LeaseGrantResponse
		cancelCtx      context.Context
		cancelFunc     context.CancelFunc
		leaseId        clientv3.LeaseID
		keepRespChan   <-chan *clientv3.LeaseKeepAliveResponse //这是一个只读channel 需要处理自动续租的结果
		txn            clientv3.Txn
		lockKey        string
		txnResp        *clientv3.TxnResponse
	)
	//1.创建租约，这个是让节点意外down机后，锁能自动释放掉
	if leaseGrantResp, err = jobLock.lease.Grant(context.TODO(), 5); err != nil {
		goto FAIL
	}
	//context用于取消自动续租
	cancelCtx, cancelFunc = context.WithCancel(context.TODO())
	//拿到租约id
	leaseId = leaseGrantResp.ID
	//2.自动续租,如果上锁成功就要一直续租不要让租约过期
	if keepRespChan, err = jobLock.lease.KeepAlive(cancelCtx, leaseId); err != nil {
		return
	}
	//3.处理自动续租
	go func() {
		var (
			keepResp *clientv3.LeaseKeepAliveResponse
		)
		for {
			select {
			case keepResp = <-keepRespChan: //自动续租应答
				if keepResp == nil { //说明自动续租被取消掉了
					goto END
				}
			}
		}
	END:
	}()

	//4.创建事务txn
	txn = jobLock.kv.Txn(context.TODO())

	//锁路径
	lockKey = common.JOB_LOCK_DIR + jobLock.jobName //抢到key了就是抢到锁了
	//5.事务抢锁
	txn.If(clientv3.Compare(clientv3.CreateRevision(lockKey), "=", 0)).
		Then(clientv3.OpPut(lockKey, "", clientv3.WithLease(leaseId))).
		Else(clientv3.OpGet(lockKey))
	//表示锁的创建版本等于0也就是如果key不存在的话就抢占这个key，值传空,创建key的时候带一个租约过去，这样节点down机后锁也会被释放。如果锁已经被占就get下什么都不做

	//提交事务
	if txnResp, err = txn.Commit(); err != nil {
		goto FAIL //提交事务失败有可能抢到有可能没有抢到，提交请求可能已经被etcd写入了，但是因为网络原因导致应答回来的时候失败，客户端不知道成不成功那我们能做的是不管实际成不成功都取消自动续约并释放租约
	}

	//6.1如果抢锁出现任何异常就回滚也就是失败释放租约
	if !txnResp.Succeeded { //锁被占用，说明上边的txn走的是Else分支
		err = common.ERR_LOCK_ALREADY_REQUIRED
		goto FAIL
	}
	//6.2上锁成功的话就return,先将租约记录下来，后边释放租约的时候会用到
	jobLock.leaseId = leaseId
	jobLock.cancelFunc = cancelFunc
	jobLock.isLocked = true
	return
FAIL: //这个也可以理解成一个回滚操作
	cancelFunc()                                  //取消自动续租
	jobLock.lease.Revoke(context.TODO(), leaseId) //主动释放租约
	return

}

//释放锁 释放锁时候要做的事情是将租约给释放掉，把自动续租给关掉 取消机制通过context
func (jobLock *JobLock) Unlock() {
	if jobLock.isLocked {
		jobLock.cancelFunc()                                  //取消我们自动续租的协程
		jobLock.lease.Revoke(context.TODO(), jobLock.leaseId) //释放租约，租约关联的key也会被删除
	}

}

//初始化一把锁，现在还没有上锁
func InitJobLock(jobName string, kv clientv3.KV, lease clientv3.Lease) (jobLock *JobLock) {
	jobLock = &JobLock{
		kv:      kv,
		lease:   lease,
		jobName: jobName,
	}
	return
}

//上锁方法
