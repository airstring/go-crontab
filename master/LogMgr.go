package master

import (
	"context"
	"crontab/common"
	"fmt"

	_ "go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

//mongodb日志管理
type LogMgr struct {
	client        *mongo.Client
	logCollection *mongo.Collection
}

//查看任务日志
func (logMgr *LogMgr) ListLog(name string, skip int, limit int) (logArr []*common.JobLog, err error) {
	var (
		filter  *common.JobLogFilter
		logSort *common.SortLogByStartTime
		cursor  *mongo.Cursor
		jobLog  *common.JobLog
	)
	//len(logArr)
	logArr = make([]*common.JobLog, 0) //防止查询的为空的时候，logArr是个空指针，所以这里先给初始化一下
	//过滤条件
	filter = &common.JobLogFilter{
		JobName: name,
	}
	//按照任务开始时间倒排
	logSort = &common.SortLogByStartTime{
		SortOrder: -1,
	}
	//查询
	if cursor, err = logMgr.logCollection.Find(context.TODO(), filter, options.Find().SetSort(logSort), options.Find().SetSkip(int64(skip)), options.Find().SetLimit(int64(limit))); err != nil {
		return
	} //返回值是游标

	//延迟释放游标
	defer cursor.Close(context.TODO())

	for cursor.Next(context.TODO()) {
		jobLog = &common.JobLog{}
		//反序列化BSON
		if err = cursor.Decode(jobLog); err != nil {
			continue //有日志不合法
		}
		fmt.Println(jobLog)
		logArr = append(logArr, jobLog)
	}

	return
}

var (
	G_logMgr *LogMgr
)

func InitLogMgr() (err error) {
	var (
		client *mongo.Client
	)
	//建立mongodb连接 , 老师加了这个查询条件options.Client().SetConnectTimeout(time.Duration(G_config.MongodbConnectTimeout))，我加后会报错，应该是其它写法
	if client, err = mongo.Connect(context.TODO(), options.Client().ApplyURI(G_config.MongodbUri)); err != nil {
		return
	}
	// Ping the primary
	if err := client.Ping(context.TODO(), readpref.Primary()); err != nil {
		panic(err)
	}
	fmt.Println("Successfully connected and pinged.")

	G_logMgr = &LogMgr{
		client:        client,
		logCollection: client.Database("cron").Collection("log"),
	}
	return
}
