package utils

import (
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// ConvertGBKToUTF8 将 GBK 编码的字节转换为 UTF-8 字符串
func ConvertGBKToUTF8(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	decoder := simplifiedchinese.GBK.NewDecoder()
	result, _, err := transform.Bytes(decoder, data)
	if err != nil {
		// 如果转换失败，尝试直接使用 UTF-8
		return string(data)
	}
	return string(result)
}

