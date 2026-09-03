package svc

func Handle() string {
	return "api"
}

// ListUsers implements GET /users (operationId ListUsers).
func ListUsers() string {
	return "[]"
}
