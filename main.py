import os

import httpx
import structlog
import typer

from cloudflare_client import CloudflareClient, CloudflareFakeClient, DNSClient
from config import AppConfig, load_config
from updater import get_public_ip, sync_all_zones

cli = typer.Typer()
logger = structlog.get_logger()


def setup_logging():
    structlog.configure(
        processors=[
            structlog.contextvars.merge_contextvars,
            structlog.stdlib.add_log_level,
            structlog.processors.TimeStamper(fmt="%Y-%m-%dT%H:%M:%S%z", key="time"),
            structlog.processors.JSONRenderer(),
        ],
    )


def main(cfg: AppConfig, http_client: httpx.Client, cf_client: DNSClient):
    ip = get_public_ip(cfg.ip_sources, client=http_client)
    if not ip:
        logger.error("All IP sources failed")
        raise typer.Exit(1)

    sync_all_zones(cf_client, cfg, ip)
    logger.info("All zones updated successfully")


@cli.command()
def entry(
    config: str = typer.Option("config.yaml", "--config", help="Path to config YAML"),
):
    setup_logging()

    cfg = load_config(config)

    with httpx.Client() as http_client:
        if cfg.dry_run:
            structlog.contextvars.bind_contextvars(dry_run=True)
            main(cfg, http_client, CloudflareFakeClient())
            return

        token = os.environ.get(cfg.token_env)
        if not token:
            logger.error("Token not found", env_var=cfg.token_env)
            raise typer.Exit(1)

        with httpx.Client(
            base_url="https://api.cloudflare.com/client/v4",
            headers={"Authorization": f"Bearer {token}"},
        ) as cf_http:
            main(cfg, http_client, CloudflareClient(cf_http))


if __name__ == "__main__":
    cli()
