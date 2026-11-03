import httpx

from config import settings


class ApiClient:
    def __init__(self):
        self._http = httpx.Client(base_url=settings.api_url)

    def get(self, endpoint):
        return self._http.get(endpoint)

    def user_orders(self, user_id):
        return self.get(f"/api-svc/v1/users/{user_id}/orders")
