package redisstore

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
)

const sessionName = "mysession"
const ok = "ok"

var newRedisStore = func(_ *testing.T) store {

	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:    []string{}, //cluster ip:port list
		Password: "",         //set password
	})

	pong, err := client.Ping().Result()
	if err != nil {
		panic(err)
	}
	fmt.Println(pong)
	store := NewRedisStore(client, []byte("secret"))
	return store
}

func init() {
	gin.SetMode(gin.TestMode)
}

func TestSessionGetSet(t *testing.T) {
	GetSet(t, newRedisStore(t))
}

func GetSet(t *testing.T, newStore sessions.Store) {
	r := gin.Default()
	r.Use(sessions.Sessions(sessionName, newRedisStore(t)))

	r.GET("/set", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("key", ok)
		session.Save()
		c.String(http.StatusOK, ok)
	})

	r.GET("/get", func(c *gin.Context) {
		session := sessions.Default(c)
		if session.Get("key") != ok {
			t.Error("Session writing failed")
		}
		c.String(http.StatusOK, ok)
	})

	res1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/set", nil)
	r.ServeHTTP(res1, req1)
	res2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/get", nil)
	req2.Header.Set("Cookie", res1.Header().Get("Set-Cookie"))
	r.ServeHTTP(res2, req2)
}
func TestSessionExpire(t *testing.T) {
	expireTime := 10
	store := newRedisStore(t)
	store.SetMaxAge(expireTime)
	r := gin.Default()
	r.Use(sessions.Sessions(sessionName, store))

	r.GET("/set", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("key", ok)
		session.Save()
		c.String(http.StatusOK, ok)
	})
	r.GET("/get", func(c *gin.Context) {
		session := sessions.Default(c)
		if session.Get("key") != ok {
			t.Error("Session writing failed")
		}
		c.String(http.StatusOK, ok)
	})
	r.GET("/get2", func(c *gin.Context) {
		session := sessions.Default(c)
		if session.Get("key") == ok {
			t.Error("Session should expire")
		}
		c.String(http.StatusOK, ok)
	})

	res1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/set", nil)
	r.ServeHTTP(res1, req1)
	res2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/get", nil)
	req2.Header.Set("Cookie", res1.Header().Get("Set-Cookie"))
	r.ServeHTTP(res2, req2)
	time.Sleep(time.Duration(expireTime) * time.Second)
	res3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/get2", nil)
	req2.Header.Set("Cookie", res1.Header().Get("Set-Cookie"))
	r.ServeHTTP(res3, req3)

}
