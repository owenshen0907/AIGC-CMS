// utils.go
package main

import (
	"encoding/json"

	"github.com/sirupsen/logrus"
)

// toJSONString 将对象转换为 JSON 字符串
func toJSONString(v interface{}) string {
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		logrus.Printf("Error marshaling to JSON: %v", err)
		return ""
	}
	return string(jsonBytes)
}
