package master

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

//如何从json配置文件中读取配置到这个结构体的字段中,通过标签语法也就是反射?
type Config struct {
	ApiPort               int      `json:"apiPort"`
	ApiReadTimeout        int      `json:"apiReadTimeout"`
	ApiWriteTimeout       int      `json:"apiWriteTimeout"`
	EtcdEndpoints         []string `json:"etcdEndpoints"`
	EtcdDialTimeout       int      `json:"etcdDialTimeout"`
	WebRoot               string   `json:"webroot"`
	MongodbUri            string   `json:"mongodbUri"`
	MongodbConnectTimeout int      `json:"mongodbConnectTimeout"`
}

var (
	//单例对象
	G_config *Config
)

//加载配置文件
func InitConfig(filename string) (err error) {
	var (
		conf    Config
		content []byte
	)
	//1.把配置文件读进来
	if content, err = ioutil.ReadFile(filename); err != nil {
		return
	}
	//2.做json反序列化
	if err = json.Unmarshal(content, &conf); err != nil {
		return
	}
	//3.赋值单例
	G_config = &conf
	fmt.Println(conf)
	return
}
