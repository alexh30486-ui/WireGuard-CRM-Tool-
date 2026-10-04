# STATUS: drafted, NOT yet run or tested.
# Mock control plane -- hardcoded response, no DB, no auth, no
# multi-tenancy. Exists only to hand the test harness a session_id.
# Unaffected by the bridge/WireGuard redesign.

from fastapi import FastAPI
from pydantic import BaseModel
import uuid

app = FastAPI()


class SessionRequest(BaseModel):
    resource_id: str
    client_public_key: str


@app.post("/api/v1/sessions/request")
async def request_session(data: SessionRequest):
    session_id = str(uuid.uuid4())
    return {
        "session_id": session_id,
        "token": "mvp_test_token_123",
        "relay_url": "ws://localhost:8080/ws/relay",
        "target_endpoint": "10.0.1.50:5432",
    }


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="127.0.0.1", port=8000)
