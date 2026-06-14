from typing import Any, Protocol, runtime_checkable

import httpx
import structlog

logger = structlog.get_logger()


@runtime_checkable
class DNSClient(Protocol):
    def get_zone_id(self, name: str) -> str | None: ...

    def list_records(self, zone_id: str, name: str, type: str) -> list[dict[str, Any]]: ...

    def update_record(self, zone_id: str, record_id: str, data: dict[str, Any]) -> None: ...

    def create_record(self, zone_id: str, data: dict[str, Any]) -> None: ...


class CloudflareClient:
    def __init__(self, http_client: httpx.Client):
        self._client = http_client

    def get_zone_id(self, name: str) -> str | None:
        resp = self._client.get("/zones", params={"name": name})
        resp.raise_for_status()
        body = resp.json()
        if not body.get("success") or not body.get("result"):
            return None
        return body["result"][0]["id"]

    def list_records(self, zone_id: str, name: str, type: str) -> list[dict[str, Any]]:
        resp = self._client.get(
            f"/zones/{zone_id}/dns_records",
            params={"name": name, "type": type},
        )
        resp.raise_for_status()
        body = resp.json()
        if not body.get("success"):
            return []
        return [
            {"id": r["id"], "content": r["content"], "name": r["name"]}
            for r in body.get("result", [])
        ]

    def update_record(self, zone_id: str, record_id: str, data: dict[str, Any]) -> None:
        resp = self._client.put(
            f"/zones/{zone_id}/dns_records/{record_id}",
            json={
                "name": data["name"],
                "type": data["type"],
                "content": data["content"],
                "ttl": data.get("ttl", 120),
                "proxied": data.get("proxied", False),
            },
        )
        resp.raise_for_status()

    def create_record(self, zone_id: str, data: dict[str, Any]) -> None:
        resp = self._client.post(
            f"/zones/{zone_id}/dns_records",
            json={
                "name": data["name"],
                "type": data["type"],
                "content": data["content"],
                "ttl": data.get("ttl", 120),
                "proxied": data.get("proxied", False),
            },
        )
        resp.raise_for_status()


class CloudflareFakeClient:
    def get_zone_id(self, name: str) -> str:
        logger.info("Resolved zone", zone=name)
        return f"dry-{name}-id"

    def list_records(self, zone_id: str, name: str, type: str) -> list[dict[str, Any]]:
        logger.info("Listed records", zone_id=zone_id, name=name, type=type)
        return []

    def update_record(self, zone_id: str, record_id: str, data: dict[str, Any]) -> None:
        logger.info("Updated record", zone_id=zone_id, record_id=record_id, data=data)

    def create_record(self, zone_id: str, data: dict[str, Any]) -> None:
        logger.info("Created record", zone_id=zone_id, data=data)
