package redis

import (
	logrusRotate "github.com/LazarenkoA/LogrusRotate"
	"github.com/garyburd/redigo/redis"
	"time"
)

type Redis struct {
	conn redis.Conn
}


func (R *Redis) Create(stringConnect string) (*Redis, error) {
	var err error
	if R.conn, err = redis.DialURL(stringConnect); err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("string connect", stringConnect).Panic("Ошибка подключения к redis")
 	}

 	return R, err
}

func (R *Redis) KeyExists(key string) bool  {
	exists, err := redis.Bool(R.conn.Do("EXISTS", key))
	if err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("key", key).Error("Redis. Ошибка при выполнении KeyExists")
	}

	return exists
}

func (R *Redis) Count(key string) int  {
	count, err := redis.Int(R.conn.Do("SCARD", key))
	if err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("key", key).Error("Redis. Ошибка при выполнении Count")
	}
	return count
}

func (R *Redis) Delete(key string) error  {
	_, err := R.conn.Do("DEL", key)
	if err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("key", key).Error("Redis. Ошибка при выполнении Delete")
	}
	return err
}

// Установка значения
// ttl - через сколько будет очищено значение (минимальное значение 1 секунда)
func (R *Redis) Set(key, value string, ttl time.Duration) error  {
	param := []interface{}{ key, value }
	if ttl >= time.Second {
		param = append(param, "EX", ttl.Seconds())
	}

	_, err := R.conn.Do("SET", param...)
	if err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("key", key).WithField("value", value).Error("Redis. Ошибка при выполнении Set")
	}
	return err
}

func (R *Redis) Get(key string) (string, error)  {
	v, err := redis.String( R.conn.Do("GET", key))
	if err != nil && err != redis.ErrNil {
		logrusRotate.StandardLogger().WithError(err).WithField("key", key).Error("Redis. Ошибка при выполнении Get")
	}
	return v, err
}

func (R *Redis) DeleteItems(key, value string) error  {
	_, err := R.conn.Do("SREM", key, value)
	if err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("key", key).WithField("value", value).Error("Redis. Ошибка при выполнении DeleteItems")
	}
	return err
}

func (R *Redis) Items(key string) []string  {
	items, err := redis.Strings(R.conn.Do("SMEMBERS", key))
	if err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("key", key).Error("Redis. Ошибка при выполнении Items")
	}
	return items
}

// Добавляет в неупорядоченную коллекцию значение
func (R *Redis) AppendItems(key, value string) {
	_, err := R.conn.Do("SADD", key, value)
	if err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("key", key).WithField("value", value).Error("Redis. Ошибка при выполнении AppendItems")
	}
}

func (R *Redis) SetMap(key string, value map[string]string) {
	for k, v := range value {
		_, err := R.conn.Do("HSET", key, k, v)
		if err != nil {
			logrusRotate.StandardLogger().WithError(err).WithField("key", key).WithField("value", value).WithField("currentValue", v).Error("Redis. Ошибка при выполнении SetMap")
			break
		}
	}
}

func (R *Redis) StringMap(key string) map[string]string  {
	value, err := redis.StringMap(R.conn.Do("HGETALL", key))
	if err != nil {
		logrusRotate.StandardLogger().WithError(err).WithField("key", key).Error("Redis. Ошибка при выполнении StringMap")
	}
	return value
}

// Начало транзакции
func (R *Redis) Begin() {
	R.conn.Do("MULTI")
}

// Фиксация транзакции
func (R *Redis) Commit() {
	R.conn.Do("EXEC")
}

// Откат транзакции
func (R *Redis) Rollback() {
	R.conn.Do("DISCARD")
}