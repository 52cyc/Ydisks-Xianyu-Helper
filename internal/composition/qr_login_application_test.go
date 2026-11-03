package composition

import "testing"

// TestBuildQRLoginApplicationWithoutRepository 验证旧零值装配不启动扫码服务；t 接收无仓储分支断言。
func TestBuildQRLoginApplicationWithoutRepository(t *testing.T) {
	// service、err 保存无账号仓储时的可选扫码服务构造结果。
	service, err := buildQRLoginApplication(Dependencies{}, nil)
	if service != nil || err != nil {
		t.Fatal("未装配账号仓储时必须保持空服务且不报错")
	}
}
