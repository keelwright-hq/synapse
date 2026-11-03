const BASE = process.env.API_URL;

export async function apiGet(path: string) {
  return fetch(`${BASE}${path}`, { method: 'GET' });
}

export async function loadUsers() {
  return apiGet('/v1/users');
}

export async function loadOrders(id: string) {
  const url = `/v1/users/${id}/orders`;
  return apiGet(url);
}
