from fastapi import FastAPI

from app.routes import scores_router

API_PREFIX = "/api/v1"

app = FastAPI()
app.include_router(scores_router, prefix=API_PREFIX)
