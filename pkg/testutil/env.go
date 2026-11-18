package testutil

import (
	"path/filepath"
	"runtime"

	"github.com/joho/godotenv"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

func init() {
	_, filename, _, _ := runtime.Caller(0)
	rel := filepath.Join(filepath.Dir(filename), "..", "..", ".env")
	godotenv.Load(rel)
	common.LoadConfig()
}
