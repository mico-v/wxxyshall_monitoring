#!/usr/bin/env python3
"""登录并保存 token，支持推送到远程服务器。

自动弹出系统浏览器(Chrome/Edge)，拦截登录响应获取 token。
安全键盘的"位置→真实字符"映射只在键位图片里，故密码由你手动点击输入。

用法:
  python3 login.py                                      # 只存本地 token.json
  python3 login.py --push http://服务器:8080             # 存本地 + 推送到服务器
  python3 login.py --browser /usr/bin/chromium           # 指定浏览器路径

环境变量:
  USTS_ADMIN_KEY  推送 token 时用的管理密钥（与服务器 ADMIN_KEY 一致）
"""
from __future__ import annotations
import asyncio
import json
import os
import sys
import time
import argparse

for _s in (sys.stdout, sys.stderr):
    try:
        _s.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

from playwright.async_api import async_playwright

TOKEN_FILE = "token.json"


def save_token(tok: dict) -> None:
    """保存 token 到本地文件。"""
    with open(TOKEN_FILE, "w", encoding="utf-8") as f:
        json.dump(tok, f, ensure_ascii=False, indent=2)
    days = (tok.get("expires_in") or 0) // 86400
    print(f"[OK] token 已保存到 {TOKEN_FILE}（学号={tok.get('sno')}，有效期约 {days} 天）")


def push_token(server_url: str, tok: dict) -> None:
    """推送 token 到远程服务器。"""
    import requests

    key = os.environ.get("USTS_ADMIN_KEY")
    if not key:
        print("[x] 需要 USTS_ADMIN_KEY 环境变量来推送 token（与服务器 ADMIN_KEY 一致）")
        sys.exit(1)

    url = server_url.rstrip("/") + "/api/token"
    r = requests.post(url, json=tok,
                      headers={"Authorization": f"Bearer {key}"},
                      timeout=20)
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
    args = parser.parse_args()

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
            "expires_in": tok.get("expires_in"),
            "login_time": int(time.time()),
            "sno": tok.get("sno"),
            "source": "network" if captured["net"] else "storage",
        }

        # 保存本地
        save_token(token_data)

        # 推送远程
        if args.push:
            push_token(args.push, token_data)

        await ctx.storage_state(path="storage_state_live.json")
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())