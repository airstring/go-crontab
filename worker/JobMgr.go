package worker

import (
	"context"
	"crontab/common"
	"fmt"
	"time"

	//	"github.com/coreos/etcd/mvcc/mvccpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

//任务管理器
type JobMgr struct {
	client  *clientv3.Client
	kv      clientv3.KV
	lease   clientv3.Lease
	watcher clientv3.Watcher
}

//监听etcd中的任务变化
func (jobMgr *JobMgr) watchJobs() (err error) {
	var (
		kvpair             *mvccpb.KeyValue
		getResp            *clientv3.GetResponse
		job                *common.Job
		watchStartRevision int64
		watchChan          clientv3.WatchChan
		watchResp          clientv3.WatchResponse
		watchEvent         *clientv3.Event
		jobName            string
		jobEvent           *common.JobEvent
	)
	//1.get一下/cron/jobs/目录下的所有任务，并且获知当前集群的revision
	if getResp, err = jobMgr.kv.Get(context.TODO(), common.JOB_SAVE_DIR, clientv3.WithPrefix()); err != nil {
		return
	}
	//当前有哪些任务
	for _, kvpair = range getResp.Kvs {
		//反序列化json得到Job
		if job, err = common.UnpackJob(kvpair.Value); err == nil {

			jobEvent = common.BuildJobEvent(common.JOB_EVENT_SAVE, job)
			//TODO:是把这个job同步给scheduler（调度协程）
			fmt.Println(*jobEvent)
			G_scheduler.PushJobEvent(jobEvent)
		}
	}
	//从该revision向后监听事件变化
	go func() {
		//从GET时刻的后续版本开始监听变化
		watchStartRevision = getResp.Header.Revision + 1
		//监听/cron/jobs/目录的后续变化
		watchChan = jobMgr.watcher.Watch(context.TODO(), common.JOB_SAVE_DIR, clientv3.WithRev(watchStartRevision), clientv3.WithPrefix())
		//处理监听事件
		for watchResp = range watchChan {
			for _, watchEvent = range watchResp.Events {
				switch watchEvent.Type {
				case mvccpb.PUT: //任务保存事件
					if job, err = common.UnpackJob(watchEvent.Kv.Value); err != nil {
						continue //出错了就忽略，出错说明etcd里保存的任务数据有问题，但是一般不会出现这个问题
					}
					//构建一个更新Event事件
					jobEvent = common.BuildJobEvent(common.JOB_EVENT_SAVE, job)
					fmt.Println(*jobEvent)
				// //TODO：反序列化Job，推送一个更新事件给scheduler
				case mvccpb.DELETE: //任务被删除了
					//Delete /cron/jobs/job10
					jobName = common.ExtractJobName(string(watchEvent.Kv.Key))
					//删除任务只需要任务名，所以这里只赋值了任务名
					job = &common.Job{Name: jobName}
					//构建一个删除Event
					jobEvent = common.BuildJobEvent(common.JOB_EVENT_DELETE, job)
					// //TODO: 推一个删除事件给scheduler

					fmt.Println(*jobEvent)
				}

				//变化事件推送给scheduler
				G_scheduler.PushJobEvent(jobEvent)
			}
		}
	}()
	return
}

//监听强杀任务通知
func (jobMgr *JobMgr) watchKiller() {
	//监听/cron/killer/目录
	go func() {
		var (
			job        *common.Job
			watchChan  clientv3.WatchChan
			watchResp  clientv3.WatchResponse
			watchEvent *clientv3.Event
			jobName    string
			jobEvent   *common.JobEvent
		)
		//监听/cron/killer/最新版本的后续变化
		watchChan = jobMgr.watcher.Watch(context.TODO(), common.JOB_KILLER_DIR, clientv3.WithPrefix())
		//处理监听事件
		for watchResp = range watchChan {
			for _, watchEvent = range watchResp.Events {
				switch watchEvent.Type {
				case mvccpb.PUT: //杀死任务事件
					//watchEvent.Kv.Key  cron/killer/job1
					jobName = common.ExtractKillerName(string(watchEvent.Kv.Key))
					job = &common.Job{Name: jobName}
					jobEvent = common.BuildJobEvent(common.JOB_EVENT_KILL, job)
					//killer事件推送给scheduler
					G_scheduler.PushJobEvent(jobEvent)
				case mvccpb.DELETE: //killer标记过期了，被自动删除,我们只关注上边的put操作
				}

			}
		}
	}()

}

//创建任务执行锁
//传入job名字，表示要锁哪个任务
func (jobMgr *JobMgr) CreateJobLock(jobName string) (jobLock *JobLock) { //返回的是一个结构体
	jobLock = InitJobLock(jobName, jobMgr.kv, jobMgr.lease)
	return
}

var (
	//单例
	G_jobMgr *JobMgr
)

//初始化管理器
func InitJobMgr() (err error) {
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
	watcher := clientv3.NewWatcher(client)
	//赋值单例
	G_jobMgr = &JobMgr{
		client:  client,
		kv:      kv,
		lease:   lease,
		watcher: watcher,
	}
	//启动任务监听
	G_jobMgr.watchJobs()
	//启动监听killer
	G_jobMgr.watchKiller()

	return
}
