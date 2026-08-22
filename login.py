"""登录并保存 token(自动弹出系统浏览器,约 70 天跑一次)。

浏览器优先用系统已装的 Google Chrome / Edge(Playwright 自动探测,跨平台免配路径),
找不到才回退 config.json 的 chromium_path。安全键盘的"位置→真实字符"映射只在
键位图片里,故密码由你在弹出的窗口里手动点击输入,脚本负责:预填学号、拦截登录响应、保存 token。

用法:
  python login.py            # 打开窗口:已填好学号,你点密码 + 登录即可
"""
from __future__ import annotations
import asyncio, sys, time

# Windows 控制台默认 GBK,统一切到 utf-8(替换不可编码字符),避免打印崩溃
for _s in (sys.stdout, sys.stderr):
    try:
        _s.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

from playwright.async_api import async_playwright
from usts_ecard.config import load_config, save_token


async def _launch_browser(p, cfg):
    """弹出可见浏览器窗口:优先系统已装的 Chrome/Edge(channel 自动探测),回退 chromium_path。"""
    common = dict(headless=False, args=["--disable-blink-features=AutomationControlled"])
    for ch in ("chrome", "msedge"):
        try:
            browser = await p.chromium.launch(channel=ch, **common)
            print(f"[*] 已弹出系统浏览器: {ch}")
            return browser
        except Exception as e:
            print(f"  [warn] 未找到 {ch}: {e}")
    ep = (cfg.get("chromium_path") or "").strip()
    if not ep or ep.startswith("填写"):
        raise SystemExit(
            "未找到系统 Chrome/Edge。请在 config.json 的 chromium_path 填写本地浏览器路径"
            "(如 Windows 的 chrome.exe,或 Linux 的 /bin/chromium)。")
    print(f"[*] 使用 config.chromium_path 指定的浏览器: {ep}")
    return await p.chromium.launch(executable_path=ep, **common)


async def main():
    cfg = load_config()
    # net = 网络拦截到的完整 token 对象(优先);store = 存储兜底(仅超时前无网络时用)
    captured = {"net": None, "store": None}

    async with async_playwright() as p:
        browser = await _launch_browser(p, cfg)
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
        await page.goto(f"{cfg['base_url']}/plat/login",
                        wait_until="networkidle", timeout=30000)

        # 预填学号
        try:
            box = page.get_by_role("textbox", name="请输入学号登录")
            await box.click()
            await box.fill(str(cfg["username"]))
            print(f"[*] 已填入学号 {cfg['username']}")
        except Exception as e:
            print(f"[warn] 自动填学号失败({e}),请在窗口里手动输入学号。")

        print("\n===== 请在弹出的浏览器窗口里完成下面两步 =====")
        print("  1) 点密码框 -> 用安全键盘点出你的密码")
        print("  2) 点【登录】")
        print("脚本会自动捕获登录结果并保存 token(最多等待 8 分钟)...\n")

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
            import re, json as _json
            jwt = re.compile(r"eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+")
            best = None
            for v in dump.values():
                if not isinstance(v, str):
                    continue
                try:  # 优先:某个值本身是包含 access_token 的对象
                    obj = _json.loads(v)
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
                # 已跳转说明登录成功;再给网络回调最多 8 秒补齐完整对象
                if left_login_at is None:
                    left_login_at = time.time()
                if time.time() - left_login_at > 8 and captured["store"] is None:
                    captured["store"] = await read_storage_token()
                    if captured["store"]:
                        print("  [ok] 网络回调未取到完整对象,已从页面存储兜底捕获 access_token。")
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

        save_token({
            "access_token": tok["access_token"],
            "expires_in": tok.get("expires_in"),
            "login_time": int(time.time()),
            "sno": tok.get("sno"),
            "source": "network" if captured["net"] else "storage",
        })
        days = (tok.get("expires_in") or 0) // 86400
        via = "网络拦截" if captured["net"] else "存储兜底"
        print(f"[OK] 登录成功({via}),token 已保存到 token.json。"
              f"学号={tok.get('sno')} 有效期约 {days} 天")
        await ctx.storage_state(path="storage_state_live.json")
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
