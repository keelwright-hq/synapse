from fastapi import APIRouter

router = APIRouter(prefix="/scores")


@router.get("/player/{player_id}")
async def player_score(player_id: str):
    return {}


@router.post(
    "/player/{player_id}/recompute")
async def recompute_score(player_id: str):
    return {}
