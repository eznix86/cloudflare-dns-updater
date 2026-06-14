import httpx
import pytest
import structlog
import typer
from typer.testing import CliRunner

from cloudflare_client import CloudflareClient, CloudflareFakeClient
from config import AppConfig, RecordConfig, ZoneConfig
from main import cli, main

runner = CliRunner()
BASE_URL = "https://api.cloudflare.com/client/v4"


def test_dry_run_succeeds_without_token(tmp_path):
    cfg = tmp_path / "config.yaml"
    cfg.write_text("dry_run: true\nzones: []")

    result = runner.invoke(cli, ["--config", str(cfg)])

    assert result.exit_code == 0


def test_missing_token_exits(tmp_path):
    cfg = tmp_path / "config.yaml"
    cfg.write_text("zones: []")

    result = runner.invoke(cli, ["--config", str(cfg)])

    assert result.exit_code == 1


def test_main_with_injected_clients():
    http_transport = httpx.MockTransport(lambda _: httpx.Response(200, text="1.2.3.4"))
    http_client = httpx.Client(transport=http_transport)

    cf_requests = []
    cf_handler = httpx.MockTransport(
        lambda req: (
            cf_requests.append((req.method, str(req.url), req.content)),
            httpx.Response(
                200,
                json=(
                    {"success": True, "result": [{"id": "zone123"}]}
                    if "/zones?" in str(req.url)
                    else {"success": True, "result": []} if req.method == "GET"
                    else {"success": True}
                ),
            ),
        )[1],
    )
    cf_client = CloudflareClient(httpx.Client(transport=cf_handler, base_url=BASE_URL))

    cfg = AppConfig(
        zones=[ZoneConfig(zone="example.com", records=[RecordConfig(name="www")])],
    )

    main(cfg, http_client, cf_client)

    assert [m for m, *_ in cf_requests] == ["GET", "GET", "POST"]


def test_main_exits_when_no_ip():
    transport = httpx.MockTransport(lambda _: httpx.Response(500, text=""))
    http_client = httpx.Client(transport=transport)

    with pytest.raises(typer.Exit) as exc:
        main(AppConfig(zones=[]), http_client, CloudflareFakeClient())
    assert exc.value.exit_code == 1


def test_setup_logging_configured():
    from main import setup_logging

    setup_logging()
    setup_logging()


def test_capture_logs():
    from main import logger

    with structlog.testing.capture_logs() as cap:
        logger.info("test msg", key="val")

    assert len(cap) == 1
    assert cap[0]["event"] == "test msg"
    assert cap[0]["key"] == "val"
    assert cap[0]["log_level"] == "info"
