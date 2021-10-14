package master

import (
	"context"
	"crontab/common"
	"encoding/json"
	"fmt"
	"time"

	//	"github.com/coreos/etcd/mvcc/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

//任务管理器
type JobMgr struct {
	client *clientv3.Client
	kv     clientv3.KV
	lease  clientv3.Lease
}

//实现一个保存方法
func (jobMgr *JobMgr) SaveJob(job *common.Job) (oldJob *common.Job, err error) {
	var (
		jobValue  []byte
		oldJobObj common.Job
	)
	//etcd的保存key
	jobKey := common.JOB_SAVE_DIR + job.Name
	fmt.Println(jobKey)
	//任务信息json
	if jobValue, err = json.Marshal(job); err != nil {
		return
	}
	//保存到etcd
	putResp, err := jobMgr.client.Put(context.TODO(), jobKey, string(jobValue), clientv3.WithPrevKV())
	if err != nil {
		return
	}
	fmt.Println("etcd save success")
	//如果是更新，那么返回旧值
	//知道了反序列化失败也没关系，上边就保存成功了，这个记录值都我们来说是次要的，所以直接赋值为空
	if putResp.PrevKv != nil {
		//对旧值做一个反序列化
		if err = json.Unmarshal(putResp.PrevKv.Value, &oldJobObj); err != nil {
			err = nil
			return
		}
		oldJob = &oldJobObj

	}
	return
}

//实现一个删除方法
func (jobMgr *JobMgr) DeleteJob(name string) (oldJob *common.Job, err error) {
	var (
		jobKey    string
		delResp   *clientv3.DeleteResponse
		OldJobObj common.Job
	)
	jobKey = common.JOB_SAVE_DIR + name

	//从etcd中删除它
	if delResp, err = jobMgr.client.Delete(context.TODO(), jobKey, clientv3.WithPrevKV()); err != nil {
		return
	}
	//返回被删除的任务信息
	if len(delResp.PrevKvs) != 0 { //如果被删除的key存在，那么这里就不为0
		//解析一下旧值返回
		if err = json.Unmarshal(delResp.PrevKvs[0].Value, &OldJobObj); err != nil {
			err = nil
			return //这里只是解析失败导致的返回，因为重点不是这里，所以忽略错误直接返回
		}
		oldJob = &OldJobObj
	}
	return
}

//列举任务
func (JobMgr *JobMgr) ListJobs() (jobList []*common.Job, err error) {
	var (
		dirKey  string
		getResp *clientv3.GetResponse
		//	kvPair *mvccpb.KeyValue
		job *common.Job
	)
	//任务保存的目录
	dirKey = common.JOB_SAVE_DIR
	//获取目录下所有任务信息
	if getResp, err = JobMgr.kv.Get(context.TODO(), dirKey, clientv3.WithPrefix()); err != nil {
		return
	}
	//初始化数组空间,初始长度为0
	jobList = make([]*common.Job, 0)
	//遍历所有任务，进行反序列化
	for _, kvPair := range getResp.Kvs {
		fmt.Println(kvPair)
		fmt.Println(string(kvPair.Value))
		job = &common.Job{}
		if err = json.Unmarshal(kvPair.Value, job); err != nil {
			err = nil //容忍个别job的反序列化失败 如果etcd的key的value不是我们特定的common.Job类型，那么就会反序列化失败

			continue
		}
		jobList = append(jobList, job) //原理是内存空间不够，就会重新分配一块更大的内存，追加前和追加后jobList的内存地址不一样了
		fmt.Println(jobList)
	}
	return
}

//杀死任务
func (JobMgr *JobMgr) KillJob(name string) (err error) {
	//更新一下key=/cron/killer/任务名
	var (
		killerKey      string
		leaseGrantResp *clientv3.LeaseGrantResponse
		leaseId        clientv3.LeaseID
	)
	//通知woerker杀死对应任务
	killerKey = common.JOB_KILLER_DIR + name
	//让worker监听到一次put操作 并给这个key设置过期时间
	if leaseGrantResp, err = JobMgr.lease.Grant(context.TODO(), 1); err != nil {
		return
	}
	leaseId = leaseGrantResp.ID
	if _, err = JobMgr.kv.Put(context.TODO(), killerKey, "", clientv3.WithLease(leaseId)); err != nil {
		return
	}

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
	//赋值单例
	G_jobMgr = &JobMgr{
		client: client,
		kv:     kv,
		lease:  lease,
	}
	return
}
