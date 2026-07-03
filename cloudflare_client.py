from typing import Any, Protocol, runtime_checkable

import httpx
import structlog

logger = structlog.get_logger()
REQUEST_RETRIES = 3
REQUEST_TIMEOUT = httpx.Timeout(30.0, connect=10.0)


@runtime_checkable
class DNSClient(Protocol):
    def get_zone_id(self, name: str) -> str | None: ...

    def list_records(self, zone_id: str, name: str, type: str) -> list[dict[str, Any]]: ...

    def update_record(self, zone_id: str, record_id: str, data: dict[str, Any]) -> None: ...

    def create_record(self, zone_id: str, data: dict[str, Any]) -> None: ...


class CloudflareClient:
    def __init__(self, http_client: httpx.Client):
        self._client = http_client

    def _request(self, method: str, url: str, **kwargs: Any) -> httpx.Response:
        for attempt in range(1, REQUEST_RETRIES + 1):
            try:
                resp = self._client.request(method, url, timeout=REQUEST_TIMEOUT, **kwargs)
                resp.raise_for_status()
                return resp
            except httpx.HTTPStatusError as exc:
                if exc.response.status_code < 500 or attempt == REQUEST_RETRIES:
                    raise
                logger.warning("Retrying Cloudflare request", method=method, url=url, attempt=attempt, error=str(exc))
            except httpx.TransportError as exc:
                if attempt == REQUEST_RETRIES:
                    raise
                logger.warning("Retrying Cloudflare request", method=method, url=url, attempt=attempt, error=str(exc))

        raise RuntimeError("unreachable")

    def get_zone_id(self, name: str) -> str | None:
        resp = self._request("GET", "/zones", params={"name": name})
        body = resp.json()
        if not body.get("success") or not body.get("result"):
            return None
        return body["result"][0]["id"]

    def list_records(self, zone_id: str, name: str, type: str) -> list[dict[str, Any]]:
        resp = self._request(
            "GET",
            f"/zones/{zone_id}/dns_records",
            params={"name": name, "type": type},
        )
        body = resp.json()
        if not body.get("success"):
            return []
        return [{"id": r["id"], "content": r["content"], "name": r["name"]} for r in body.get("result", [])]

    def update_record(self, zone_id: str, record_id: str, data: dict[str, Any]) -> None:
        self._request(
            "PUT",
            f"/zones/{zone_id}/dns_records/{record_id}",
            json={
                "name": data["name"],
                "type": data["type"],
                "content": data["content"],
                "ttl": data.get("ttl", 120),
                "proxied": data.get("proxied", False),
            },
        )

    def create_record(self, zone_id: str, data: dict[str, Any]) -> None:
        self._request(
            "POST",
            f"/zones/{zone_id}/dns_records",
            json={
                "name": data["name"],
                "type": data["type"],
                "content": data["content"],
                "ttl": data.get("ttl", 120),
                "proxied": data.get("proxied", False),
            },
        )


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
