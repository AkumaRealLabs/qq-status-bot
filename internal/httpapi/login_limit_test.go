package httpapi

import (
	"fmt"
	"testing"
	"time"
)

func TestLoginLimiterBlocksAfterThreshold(t *testing.T) {
	s := &Server{}
	key := "10.0.0.1|admin"
	for i := 0; i < loginFailLimit; i++ {
		if s.loginLimited(key) {
			t.Fatalf("第 %d 次失败前不应该被限流", i+1)
		}
		s.recordLoginFailure(key)
	}
	if !s.loginLimited(key) {
		t.Fatalf("累计 %d 次失败后应该被限流", loginFailLimit)
	}
	s.clearLoginFailures(key)
	if s.loginLimited(key) {
		t.Fatal("登录成功清空后不应再限流")
	}
}

// key 里含攻击者可控的用户名，过期后必须真正从 map 删除而不是留空切片。
func TestLoginLimiterDropsExpiredKey(t *testing.T) {
	s := &Server{}
	key := "10.0.0.1|admin"
	s.recordLoginFailure(key)
	if len(s.loginFails) != 1 {
		t.Fatalf("记录失败后应有 1 个 key，实际 %d", len(s.loginFails))
	}
	s.loginFails[key] = []time.Time{time.Now().Add(-loginFailWindow - time.Minute)}
	if s.loginLimited(key) {
		t.Fatal("窗口外的失败记录不应触发限流")
	}
	if len(s.loginFails) != 0 {
		t.Fatalf("过期 key 应被删除，实际残留 %d 个", len(s.loginFails))
	}
}

func TestLoginLimiterSweepsAtKeyLimit(t *testing.T) {
	s := &Server{loginFails: map[string][]time.Time{}}
	expired := []time.Time{time.Now().Add(-loginFailWindow - time.Minute)}
	for i := 0; i < loginFailKeyLimit; i++ {
		s.loginFails[fmt.Sprintf("10.0.0.1|user-%d", i)] = expired
	}
	fresh := "10.0.0.1|user-fresh"
	s.loginFails[fresh] = []time.Time{time.Now()}

	s.recordLoginFailure("10.0.0.2|admin")

	if len(s.loginFails) != 2 {
		t.Fatalf("清扫后应只剩窗口内的 2 个 key，实际 %d", len(s.loginFails))
	}
	if _, ok := s.loginFails[fresh]; !ok {
		t.Fatal("清扫不应删掉窗口内仍有失败记录的 key")
	}
}
