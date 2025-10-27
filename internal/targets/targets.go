package targets

import (
	"net"
	"net/url"
	"strings"
)

// Normalize 对用户输入的地址进行裁剪并提取主机部分。
func Normalize(address string) string {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return ""
	}

	// 处理带协议前缀的输入。
	if strings.Contains(addr, "://") {
		if u, err := url.Parse(addr); err == nil {
			if u.Host != "" {
				addr = u.Host
			}
		}
	}

	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "//")

	// 去除可能存在的账号密码片段（user:pass@host）。
	if at := strings.LastIndex(addr, "@"); at != -1 {
		addr = addr[at+1:]
	}

	// 去除剩余的路径或查询参数。
	if slash := strings.IndexByte(addr, '/'); slash != -1 {
		addr = addr[:slash]
	}
	if ques := strings.IndexByte(addr, '?'); ques != -1 {
		addr = addr[:ques]
	}

	addr = strings.TrimSpace(addr)

	// 支持形如 [::1]:443 或 [::1] 的 IPv6 写法。
	if strings.HasPrefix(addr, "[") && strings.Contains(addr, "]") {
		end := strings.Index(addr, "]")
		if end != -1 {
			addr = addr[1:end]
		}
	}

	// 对 IPv4 或单冒号 host:port 的写法剥离端口，避免与 IPv6 冲突。
	if strings.Count(addr, ":") == 1 {
		if host, _, err := net.SplitHostPort(addr); err == nil {
			addr = host
		}
	}

	return strings.ToLower(strings.Trim(addr, "[] "))
}

// Build 将输入解析为唯一的扫描目标列表。
// 返回内容包括标准化主机名以及解析得到的 IP。
func Build(address string) []string {
	normalized := Normalize(address)
	if normalized == "" {
		return nil
	}

	seen := make(map[string]struct{})
	result := make([]string, 0, 4)
	appendUnique := func(val string) {
		if val == "" {
			return
		}
		if _, ok := seen[val]; ok {
			return
		}
		seen[val] = struct{}{}
		result = append(result, val)
	}

	appendUnique(normalized)

	if net.ParseIP(normalized) == nil {
		if ips, err := net.LookupHost(normalized); err == nil {
			for _, ip := range ips {
				if net.ParseIP(ip) != nil {
					appendUnique(ip)
				}
			}
		}
	}

	return result
}
