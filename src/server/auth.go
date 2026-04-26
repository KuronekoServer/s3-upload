package server

import "net/http"

// authMiddleware は X-Auth-Key ヘッダーを検証するミドルウェアです。
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authEnabled {
			key := r.Header.Get("X-Auth-Key")
			if key == "" || key != s.authKey {
				writeJSON(w, http.StatusUnauthorized, APIResponse{Success: false, Message: "認証に失敗しました"})
				return
			}
		}

		next(w, r)
	}
}
