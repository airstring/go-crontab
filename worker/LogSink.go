package worker

import (
	"context"
	"crontab/common"
	"time"

	_ "go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

//mongodb存储日志
type LogSink struct {
	client         *mongo.Client
	logCollection  *mongo.Collection
	logChan        chan *common.JobLog
	autoCommitChan chan *common.LogBatch
}

//批量写入日志
func (logSink *LogSink) saveLogs(batch *common.LogBatch) {
	logSink.logCollection.InsertMany(context.TODO(), batch.Logs)
}

//日志存储协程
func (logSink *LogSink) writeLoop() {
	var (
		log          *common.JobLog
		logBatch     *common.LogBatch
		commitTimer  *time.Timer
		timeoutBatch *common.LogBatch //超时批次

	)

	for {
		select {
		case log = <-G_logSink.logChan:
			//每次插入需要等待mongodb的一次请求往返，耗时可能因为网络慢话费比较长的时间，所以积攒多次后再插入
			if logBatch == nil {
				logBatch = &common.LogBatch{}
				//让这个批次超时自动提交(给1s的时间)
				commitTimer = time.AfterFunc(
					time.Duration(G_config.JobLogCommitTimeout)*time.Millisecond, //睡眠一秒钟
					//超时后执行一个回调函数 会在另外一个协程中执行这个回调函数，就涉及到一个并发问题，其它的协程在提交这个batch。writeLoop和当前这个func是两个不同的协程，对同一个batch操作就是并发操作。所以有了下边这个闭包
					func(batch *common.LogBatch) func() {
						return func() { //在一个函数内部生成了一个函数，这个函数才是定时器的函数，到期后会执行这个函数
							logSink.autoCommitChan <- batch //batch 这个batch是一个新的地址，和外边的batch不一样了。
						}
					}(logBatch), //传入的是一个地址
				)
			}
			//把新日志追加到批次中
			logBatch.Logs = append(logBatch.Logs, log)
			//如果批次满了，就立即发送
			if len(logBatch.Logs) >= G_config.JobLogBatchSize {
				//发送日志
				logSink.saveLogs(logBatch)
				//清空logBatch
				logBatch = nil
				//取消定时器
				commitTimer.Stop()
			}

		case timeoutBatch = <-logSink.autoCommitChan: //过期的批次
			//有可能刚好1s中的时候过期batch被投递给管道并被timeoutBatch接收并且1s中的时候刚好满G_config.JobLogBatchSize。注意这个G_config.JobLogCommitTimeout临界点。
			if timeoutBatch != logBatch { //判断过期批次是否仍然是当前的批次  select语法中，这个case执行上边的case就不会同时执行。
				continue //说明1s钟的时候，当前批次满了被正常写入mongo，过期批次做个让步不重复写了
			}
			//把批次写入到mongo中
			logSink.saveLogs(timeoutBatch)
			//清空logBatch
			logBatch = nil //这个是清空当前批次
		}

	}
}

var (
	//单例
	G_logSink *LogSink
)

func InitLogSink() (err error) {
	var (
		client *mongo.Client
	)
	//建立mongodb连接
	if client, err = mongo.Connect(context.TODO(), options.Client().ApplyURI(G_config.MongodbUri)); err != nil {
		return
	}
	G_logSink = &LogSink{
		client:         client,
		logCollection:  client.Database("cron").Collection("log"),
		logChan:        make(chan *common.JobLog, 1000),
		autoCommitChan: make(chan *common.LogBatch, 1000),
	}
	//启动一个mongodb处理协程
	go G_logSink.writeLoop()
	return
}

//发送日志
func (logSink *LogSink) Append(jobLog *common.JobLog) {
	select {
	case logSink.logChan <- jobLog: //队列满了这个地方就会阻塞，就会执行default语句
	default:
		//队列满了就丢弃

	}
}
