import axios from 'axios';

const scoresClient = axios.create({ baseURL: window.location.origin });

export async function playerScore(id: string) {
  return scoresClient.get(`/scores/player/${encodeURIComponent(id)}`);
}
