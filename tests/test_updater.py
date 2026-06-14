import json

import httpx

from cloudflare_client import CloudflareClient
from config import AppConfig, RecordConfig, ZoneConfig
from updater import get_public_ip, resolve_zone, sync_all_zones, sync_record

BASE_URL = "https://api.cloudflare.com/client/v4"

ZONE_RESPONSE = {"success": True, "result": [{"id": "zone123", "name": "example.com"}]}
ZONE_MISSING_RESPONSE = {"success": True, "result": []}
RECORDS_RESPONSE = {"success": True, "result": []}
SUCCESS_RESPONSE = {"success": True}


class TestGetPublicIP:
    def test_returns_first_working_source(self):
        transport = httpx.MockTransport(lambda _: httpx.Response(200, text="1.2.3.4"))
        with httpx.Client(transport=transport) as client:
            ip = get_public_ip(["https://example.com/ip"], client=client)
        assert ip == "1.2.3.4"

    def test_skips_failed_sources(self):
        responses = iter([
            httpx.Response(500, text=""),
            httpx.Response(200, text="5.6.7.8"),
        ])
        transport = httpx.MockTransport(lambda _: next(responses))
        with httpx.Client(transport=transport) as client:
            ip = get_public_ip(["https://fail.com", "https://ok.com"], client=client)
        assert ip == "5.6.7.8"

    def test_returns_none_when_all_fail(self):
        transport = httpx.MockTransport(lambda _: httpx.Response(500, text=""))
        with httpx.Client(transport=transport) as client:
            ip = get_public_ip(["https://fail.com"], client=client)
        assert ip is None

    def test_skips_on_exception_and_moves_to_next(self):
        def handler(req: httpx.Request) -> httpx.Response:
            if "fail" in str(req.url):
                raise httpx.ConnectError("connection refused")
            return httpx.Response(200, text="5.6.7.8")

        transport = httpx.MockTransport(handler)
        with httpx.Client(transport=transport) as client:
            ip = get_public_ip(["https://fail.com", "https://ok.com"], client=client)
        assert ip == "5.6.7.8"


class TestResolveZone:
    def test_returns_zone_id(self):
        transport = httpx.MockTransport(
            lambda _: httpx.Response(200, json=ZONE_RESPONSE),
        )
        with httpx.Client(transport=transport, base_url=BASE_URL) as client:
            cf = CloudflareClient(client)
            assert resolve_zone(cf, "example.com") == "zone123"

    def test_returns_none_when_not_found(self):
        transport = httpx.MockTransport(
            lambda _: httpx.Response(200, json=ZONE_MISSING_RESPONSE),
        )
        with httpx.Client(transport=transport, base_url=BASE_URL) as client:
            cf = CloudflareClient(client)
            assert resolve_zone(cf, "example.com") is None


class TestSyncRecord:
    def test_skips_when_ip_unchanged(self):
        requests = []
        transport = httpx.MockTransport(
            lambda req: (requests.append((req.method, str(req.url))) or httpx.Response(
                200,
                json={"success": True, "result": [{"id": "rec123", "content": "1.2.3.4", "name": "www.example.com"}]},
            )),
        )
        with httpx.Client(transport=transport, base_url=BASE_URL) as client:
            cf = CloudflareClient(client)
            sync_record(cf, "zone123", "example.com", RecordConfig(name="www"), "1.2.3.4")

        assert len(requests) == 1
        assert requests[0][0] == "GET"
        assert "name=www.example.com" in requests[0][1]

    def test_updates_when_ip_changed(self):
        requests = []
        handler = httpx.MockTransport(
            lambda req: (
                requests.append((req.method, str(req.url), req.content)),
                httpx.Response(
                    200,
                    json=(
                        {"success": True, "result": [{"id": "rec123", "content": "9.9.9.9", "name": "www.example.com"}]}
                        if req.method == "GET"
                        else SUCCESS_RESPONSE
                    ),
                ),
            )[1],
        )
        with httpx.Client(transport=handler, base_url=BASE_URL) as client:
            cf = CloudflareClient(client)
            sync_record(cf, "zone123", "example.com", RecordConfig(name="www", ttl=300, proxied=True), "1.2.3.4")

        assert len(requests) == 2
        assert [m for m, *_ in requests] == ["GET", "PUT"]
        payload = json.loads(requests[1][2])
        assert payload == {"type": "A", "name": "www.example.com", "content": "1.2.3.4", "ttl": 300, "proxied": True}

    def test_creates_when_no_record(self):
        requests = []
        handler = httpx.MockTransport(
            lambda req: (
                requests.append((req.method, str(req.url), req.content)),
                httpx.Response(
                    200,
                    json=(
                        RECORDS_RESPONSE
                        if req.method == "GET"
                        else SUCCESS_RESPONSE
                    ),
                ),
            )[1],
        )
        with httpx.Client(transport=handler, base_url=BASE_URL) as client:
            cf = CloudflareClient(client)
            sync_record(cf, "zone123", "example.com", RecordConfig(name="www"), "1.2.3.4")

        assert len(requests) == 2
        assert [m for m, *_ in requests] == ["GET", "POST"]
        payload = json.loads(requests[1][2])
        assert payload == {"type": "A", "name": "www.example.com", "content": "1.2.3.4", "ttl": 120, "proxied": False}


class TestSyncAllZones:
    def test_syncs_all_zones_and_records(self):
        requests = []
        handler = httpx.MockTransport(
            lambda req: (
                requests.append((req.method, str(req.url), req.content)),
                httpx.Response(
                    200,
                    json=(
                        {"success": True, "result": [{"id": "zone123"}]}
                        if "/zones?" in str(req.url) and "/dns_records" not in str(req.url)
                        else RECORDS_RESPONSE if req.method == "GET"
                        else SUCCESS_RESPONSE
                    ),
                ),
            )[1],
        )
        with httpx.Client(transport=handler, base_url=BASE_URL) as client:
            cf = CloudflareClient(client)

            config = AppConfig(
                zones=[
                    ZoneConfig(zone="example.com", records=[
                        RecordConfig(name="www"),
                        RecordConfig(name="api"),
                    ]),
                ],
            )

            sync_all_zones(cf, config, "1.2.3.4")

        assert [m for m, *_ in requests] == ["GET", "GET", "POST", "GET", "POST"]

    def test_skips_zone_when_not_found(self):
        requests = []
        handler = httpx.MockTransport(
            lambda req: (
                requests.append((req.method, str(req.url), req.content)),
                httpx.Response(
                    200,
                    json=(
                        ZONE_MISSING_RESPONSE
                        if "missing.com" in str(req.url)
                        else {"success": True, "result": [{"id": "zone456"}]}
                        if "/zones?" in str(req.url)
                        else RECORDS_RESPONSE if req.method == "GET"
                        else SUCCESS_RESPONSE
                    ),
                ),
            )[1],
        )
        with httpx.Client(transport=handler, base_url=BASE_URL) as client:
            cf = CloudflareClient(client)

            config = AppConfig(
                zones=[
                    ZoneConfig(zone="missing.com", records=[RecordConfig(name="www")]),
                    ZoneConfig(zone="example.com", records=[RecordConfig(name="api")]),
                ],
            )

            sync_all_zones(cf, config, "1.2.3.4")

        assert [m for m, *_ in requests] == ["GET", "GET", "GET", "POST"]
