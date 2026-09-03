package svc

func Handle() string {
	return "worker"
}

// FetchUsers calls the users API.
func FetchUsers() string {
	url := "/users"
	return url
}

// Users consumes GraphQL Query.users by operation name match.
func Users() string {
	return "users"
}

// CallListUsers consumes gRPC UserService.ListUsers via method path literal.
func CallListUsers() string {
	method := "/users.UserService/ListUsers"
	return method
}
