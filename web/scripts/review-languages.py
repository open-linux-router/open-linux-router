"""Browser regression review; requires Python Playwright and installed Chromium."""
import threading
from functools import partial
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from playwright.sync_api import sync_playwright

root = Path(__file__).resolve().parents[2] / 'internal/webui/assets'
class Static(SimpleHTTPRequestHandler):
    def do_GET(self):
        if not (root / self.path.lstrip('/')).is_file():
            self.path = '/index.html'
        super().do_GET()
    def log_message(self, *args):
        pass
server = ThreadingHTTPServer(('127.0.0.1', 0), partial(Static, directory=str(root)))
threading.Thread(target=server.serve_forever, daemon=True).start()
out = Path('/tmp/olr-language-review')
out.mkdir(exist_ok=True)
errors = []
with sync_playwright() as pw:
    browser = pw.chromium.launch(args=['--no-sandbox'])
    for theme in ['light', 'dark']:
        for width in [375, 768, 1440]:
            context = browser.new_context(viewport=dict(width=width, height=1000), color_scheme=theme, locale='zh-CN')
            page = context.new_page()
            page.on('pageerror', lambda error: errors.append(str(error)))
            context.route('**/api/**', lambda route: route.fulfill(json=dict(claimed=False)))
            page.goto(f'http://127.0.0.1:{server.server_port}/', wait_until='networkidle')
            assert page.locator('html').get_attribute('lang') == 'zh-CN'
            page.get_by_role('heading', name='初始化路由器').wait_for()
            page.locator('#setup-password').fill('unsaved-password')
            page.get_by_role('button', name='切换为 English').click()
            page.get_by_role('heading', name='Set up this router').wait_for()
            assert page.locator('#setup-password').input_value() == 'unsaved-password'
            assert not page.evaluate('document.documentElement.scrollWidth > innerWidth')
            second = context.new_page()
            second.goto(f'http://127.0.0.1:{server.server_port}/', wait_until='networkidle')
            assert second.locator('html').get_attribute('lang') == 'en'
            second.get_by_role('button', name='Switch language to Chinese').click()
            page.get_by_role('heading', name='初始化路由器').wait_for()
            page.reload(wait_until='networkidle')
            page.get_by_role('heading', name='初始化路由器').wait_for()
            page.screenshot(path=str(out / f'setup-{theme}-{width}.png'), full_page=True)
            page.route('**/api/**', lambda route: route.fulfill(json=dict(claimed=True, password_set=False, authenticated=True) if route.request.url.endswith('/api/system/access') else {}))
            page.goto(f'http://127.0.0.1:{server.server_port}/wireless', wait_until='networkidle')
            page.get_by_text('无线设置即将推出。').wait_for()
            assert not page.evaluate('document.documentElement.scrollWidth > innerWidth')
            page.screenshot(path=str(out / f'shell-{theme}-{width}.png'), full_page=True)
            page.get_by_role('button', name='切换为 English').click()
            page.get_by_text('Wireless settings are coming soon.').wait_for()
            context.close()
    browser.close()
server.shutdown()
assert not errors, errors
print(f'Language switching, forms, persistence, tabs and responsive layouts passed. Screenshots: {out}')
