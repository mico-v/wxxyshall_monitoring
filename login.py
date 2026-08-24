#!/usr/bin/env python3
"""登录并保存 token，支持推送到远程服务器。

自动弹出系统浏览器(Chrome/Edge)，拦截登录响应获取 token。
安全键盘的"位置→真实字符"映射只在键位图片里，故密码由你手动点击输入。

用法:
  python3 login.py                                      # 保存到 token.json（或 ELEc_DIR/data/token.json）
  python3 login.py --output /opt/elec/data/token.json    # 指定本地保存位置
  python3 login.py --push http://服务器:8080             # 存本地 + 推送到服务器
  python3 login.py --push http://服务器:8080 --push-only # 只推送，不在本地落盘
  python3 login.py --browser /usr/bin/chromium           # 指定浏览器路径

环境变量:
  ADMIN_KEY       推送 token 时用的管理密钥
"""
from __future__ import annotations
import asyncio
import json
import os
import sys
import time
import argparse
import tempfile
from pathlib import Path

for _s in (sys.stdout, sys.stderr):
    try:
        _s.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

from playwright.async_api import async_playwright

def default_token_file() -> Path:
    root = os.environ.get("ELEc_DIR")
    return Path(root) / "data" / "token.json" if root else Path("token.json")


def save_token(tok: dict, output: Path) -> None:
    """保存 token 到本地文件。"""
    output = output.expanduser().resolve()
    output.parent.mkdir(parents=True, exist_ok=True, mode=0o750)
    fd, temp_name = tempfile.mkstemp(prefix=".token-", dir=output.parent)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(tok, f, ensure_ascii=False, indent=2)
            f.write("\n")
            f.flush()
            os.fsync(f.fileno())
        os.replace(temp_name, output)
        os.chmod(output, 0o600)
    except Exception:
        try:
            os.close(fd)
        except OSError:
            pass
        try:
            os.unlink(temp_name)
        except FileNotFoundError:
            pass
        raise
    days = (tok.get("expires_in") or 0) // 86400
    print(f"[OK] token 已保存到 {output}（学号={tok.get('sno')}，有效期约 {days} 天）")


def push_token(server_url: str, tok: dict, admin_key_file: str | None) -> None:
    """推送 token 到远程服务器。"""
    import requests

    key = ""
    if admin_key_file:
        try:
            key = Path(admin_key_file).expanduser().read_text(encoding="utf-8").strip()
        except OSError as e:
            raise SystemExit(f"[x] 读取管理密钥文件失败: {e}") from e
    if not key:
        key = os.environ.get("ADMIN_KEY", "").strip()
    if not key:
        print("[x] 需要 ADMIN_KEY 环境变量来推送 token（与服务器 ADMIN_KEY 一致）")
        sys.exit(1)

    url = server_url.rstrip("/") + "/api/token"
    r = requests.post(url, json=tok,
                      headers={"Authorization": f"Bearer {key}"},
                      timeout=20, allow_redirects=False)
    try:
        j = r.json()
    except ValueError:
        j = {}
    if r.status_code != 200:
        print(f"[x] 推送失败 HTTP {r.status_code}: {j.get('error', r.text[:200])}")
        sys.exit(1)
    print(f"[OK] token 已推送到 {url}（有效期约 {j.get('expires_in_days')} 天）")


async def _launch_browser(p, browser_path: str | None):
    """弹出可见浏览器窗口。"""
    common = dict(headless=False, args=["--disable-blink-features=AutomationControlled"])

    if browser_path:
        print(f"[*] 使用指定浏览器: {browser_path}")
        return await p.chromium.launch(executable_path=browser_path, **common)

    for ch in ("chrome", "msedge"):
        try:
            browser = await p.chromium.launch(channel=ch, **common)
            print(f"[*] 已弹出系统浏览器: {ch}")
            return browser
        except Exception as e:
            print(f"  [warn] 未找到 {ch}: {e}")

    raise SystemExit(
        "未找到系统 Chrome/Edge。请安装 Chrome 或 Edge，"
        "或用 --browser 参数指定路径。"
    )


async def main():
    parser = argparse.ArgumentParser(description="登录并获取 token")
    parser.add_argument("--push", metavar="URL", help="推送 token 到服务器地址")
    parser.add_argument("--browser", metavar="PATH", help="浏览器可执行文件路径")
    parser.add_argument("--output", metavar="PATH", help="本地 token 输出路径")
    parser.add_argument("--push-only", action="store_true", help="只推送，不保存本地 token")
    parser.add_argument("--admin-key-file", metavar="PATH", help="从文件读取推送所需管理密钥")
    parser.add_argument("--storage-state", metavar="PATH", help="可选：保存浏览器 storage state（含敏感登录态）")
    args = parser.parse_args()
    if args.push_only and not args.push:
        parser.error("--push-only 必须与 --push 一起使用")

    captured = {"net": None, "store": None}

    async with async_playwright() as p:
        browser = await _launch_browser(p, args.browser)
        ctx = await browser.new_context(**p.devices["Pixel 5"], locale="zh-CN")
        page = await ctx.new_page()

        async def on_response(resp):
            u = resp.url
            if "/berserker-auth/oauth/token" in u and resp.status == 200:
                print(f"  [net] {resp.request.method} {resp.status} /berserker-auth/oauth/token")
                try:
                    captured["net"] = await resp.json()
                except Exception as e:
                    print("  [warn] 解析 token 响应失败:", e)

        page.on("response", on_response)

        print("[*] 打开登录页 ...")
        base_url = "https://wxxyshall.usts.edu.cn"
        await page.goto(f"{base_url}/plat/login",
                        wait_until="networkidle", timeout=30000)

        print("\n===== 请在弹出的浏览器窗口里完成下面两步 =====")
        print("  1) 点密码框 -> 用安全键盘点出你的密码")
        print("  2) 点【登录】")
        print("脚本会自动捕获登录结果(最多等待 8 分钟)...\n")

        async def read_storage_token():
            try:
                dump = await page.evaluate("""() => {
                    const all = {};
                    for (const s of [localStorage, sessionStorage])
                        for (let i=0;i<s.length;i++){const k=s.key(i);all[k]=s.getItem(k);}
                    return all;
                }""")
            except Exception:
                return None
            import re
            jwt = re.compile(r"eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+")
            best = None
            for v in dump.values():
                if not isinstance(v, str):
                    continue
                try:
                    obj = json.loads(v)
                    if isinstance(obj, dict) and obj.get("access_token"):
                        return obj
                except Exception:
                    pass
                m = jwt.search(v)
                if m and best is None:
                    best = {"access_token": m.group(0)}
            return best

        deadline = time.time() + 480
        left_login_at = None
        last_beat = 0
        while time.time() < deadline and not captured["net"]:
            await page.wait_for_timeout(500)
            if "/login" not in page.url:
                if left_login_at is None:
                    left_login_at = time.time()
                if time.time() - left_login_at > 8 and captured["store"] is None:
                    captured["store"] = await read_storage_token()
                    if captured["store"]:
                        print("  [ok] 从页面存储兜底捕获 access_token")
                    break
            now = time.time()
            if now - last_beat > 30:
                last_beat = now
                print(f"  [..] 等待中,剩余 {int(deadline-now)} 秒。当前页面: {page.url}")

        tok = captured["net"] or captured["store"]
        if not tok or not tok.get("access_token"):
            print("[x] 未捕获到登录 token(超时或未完成登录)。")
            await browser.close()
            sys.exit(1)

        token_data = {
            "access_token": tok["access_token"],
            "expires_in": int(tok.get("expires_in") or 0),
            "login_time": int(time.time()),
            "sno": str(tok.get("sno") or ""),
            "source": "network" if captured["net"] else "storage",
        }
        if tok.get("refresh_token"):
            token_data["refresh_token"] = tok["refresh_token"]

        # 保存本地
        if not args.push_only:
            save_token(token_data, Path(args.output) if args.output else default_token_file())

        # 推送远程
        if args.push:
            push_token(args.push, token_data, args.admin_key_file)

        if args.storage_state:
            old_umask = os.umask(0o077)
            try:
                await ctx.storage_state(path=args.storage_state)
            finally:
                os.umask(old_umask)
            try:
                os.chmod(args.storage_state, 0o600)
            except OSError as e:
                print(f"  [warn] 无法收紧 storage state 权限: {e}")
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
