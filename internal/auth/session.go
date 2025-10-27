package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/hitushen/portnotepro/internal/store"
)

const sessionName = "portnote_auth"

// Manager 负责处理登录会话。
type Manager struct {
	store  *store.Store
	cookie sessions.Store
}

// NewManager 使用提供的会话密钥创建 Manager。
func NewManager(store *store.Store, sessionKey []byte) *Manager {
	cookieStore := sessions.NewCookieStore(sessionKey)
	cookieStore.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   60 * 60 * 12, // 12 小时
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	return &Manager{
		store:  store,
		cookie: cookieStore,
	}
}

// Authenticate 校验凭证并写入会话信息。
func (m *Manager) Authenticate(w http.ResponseWriter, r *http.Request, username, password string) error {
	user, err := m.store.Authenticate(r.Context(), username, password)
	if err != nil {
		return err
	}
	session, _ := m.cookie.Get(r, sessionName)
	session.Values["user_id"] = user.ID
	session.Values["username"] = user.Username
	return session.Save(r, w)
}

// Logout 清理当前会话。
func (m *Manager) Logout(w http.ResponseWriter, r *http.Request) error {
	session, _ := m.cookie.Get(r, sessionName)
	session.Options.MaxAge = -1
	return session.Save(r, w)
}

// RequireUser 提取当前登录用户的 ID。
func (m *Manager) RequireUser(w http.ResponseWriter, r *http.Request) (int64, error) {
	session, err := m.cookie.Get(r, sessionName)
	if err != nil {
		return 0, err
	}
	raw := session.Values["user_id"]
	userID := toInt64(raw)
	if userID == 0 {
		return 0, errors.New("unauthorised")
	}
	return userID, nil
}

// Middleware 确保请求具备已登录用户。
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := m.cookie.Get(r, sessionName)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if _, ok := session.Values["user_id"]; !ok {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Username 获取用户名以供界面展示。
func (m *Manager) Username(r *http.Request) string {
	session, err := m.cookie.Get(r, sessionName)
	if err != nil {
		return ""
	}
	if uname, ok := session.Values["username"].(string); ok {
		return uname
	}
	return ""
}

// ContextWithUser 将用户 ID 写入上下文。
func ContextWithUser(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, contextKey("user_id"), userID)
}

// UserFromContext 从上下文读取用户 ID。
func UserFromContext(ctx context.Context) (int64, bool) {
	val := ctx.Value(contextKey("user_id"))
	if val == nil {
		return 0, false
	}
	id, ok := val.(int64)
	return id, ok
}

type contextKey string

func toInt64(v interface{}) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case uint:
		return int64(value)
	case uint64:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}
