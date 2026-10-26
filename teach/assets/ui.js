/* Shell: daisyUI drawer + navbar + sidebar menu, dark theme toggle.
   Builds the layout at runtime so content pages stay plain HTML. */
(function () {
  var path = decodeURIComponent(location.pathname);
  var inSub = /\/(lessons|reference)\//.test(path);
  var up = inSub ? '../' : '';
  var gh = 'https://github.com/daniel-d7a/ZeroOps/blob/main/teach/';
  var groups = [
    { label: 'Lessons', items: [
      { href: up + 'index.html', file: 'index.html', label: 'Home · start here' },
      { href: up + 'lessons/0001-architecture-30000ft.html', file: '0001-architecture-30000ft.html', label: '1 · The 30,000-ft tour' },
      { href: up + 'lessons/0002-finite-state-machine.html', file: '0002-finite-state-machine.html', label: '2 · Finite state machine' },
    ]},
    { label: 'Reference', items: [
      { href: up + 'reference/architecture.html', file: 'architecture.html', label: 'Architecture map' },
      { href: up + 'reference/deploy-lifecycle.html', file: 'deploy-lifecycle.html', label: 'Deploy lifecycle' },
      { href: up + 'reference/queue-topology.html', file: 'queue-topology.html', label: 'Queue topology' },
      { href: up + 'reference/api-map.html', file: 'api-map.html', label: 'API map' },
      { href: up + 'reference/worker-map.html', file: 'worker-map.html', label: 'Worker map' },
      { href: up + 'reference/glossary.html', file: 'glossary.html', label: 'Glossary' },
    ]},
  ];
  var cur = path.split('/').pop();
  if (!cur || !/\.html$/.test(cur)) cur = 'index.html';

  var html = document.documentElement;

  /* ---------- theme ---------- */
  var THEME_KEY = 'zeroops.theme';
  function applyTheme(t) {
    html.setAttribute('data-theme', t);
    try { localStorage.setItem(THEME_KEY, t); } catch (e) {}
    var btn = document.getElementById('theme-toggle');
    if (btn) {
      btn.textContent = t === 'dark' ? '☀' : '☾';
      btn.title = t === 'dark' ? 'Switch to light' : 'Switch to dark';
    }
  }
  var savedTheme = null;
  try { savedTheme = localStorage.getItem(THEME_KEY); } catch (e) {}
  applyTheme(savedTheme || 'dark');

  /* ---------- shell ---------- */
  var drawer = document.createElement('div');
  drawer.className = 'drawer lg:drawer-open';
  drawer.innerHTML =
    '<input id="nav-drawer" type="checkbox" class="drawer-toggle" />' +
    '<div class="drawer-content flex min-h-screen flex-col">' +
      '<header class="navbar sticky top-0 z-40 gap-2 border-b border-base-300 bg-base-100/90 px-2 backdrop-blur sm:px-4">' +
        '<label for="nav-drawer" class="btn btn-square btn-ghost lg:hidden" aria-label="Open navigation">☰</label>' +
        '<div class="flex-1 truncate px-1 text-sm font-semibold tracking-tight sm:text-base">ZeroOps · teaching workspace</div>' +
        '<button id="theme-toggle" type="button" class="btn btn-square btn-ghost btn-sm" aria-label="Toggle theme"></button>' +
      '</header>' +
      '<main class="mx-auto w-full max-w-4xl flex-1 px-4 py-8 sm:px-6 lg:px-10 lg:py-10"></main>' +
    '</div>' +
    '<div class="drawer-side z-50">' +
      '<label for="nav-drawer" class="drawer-overlay" aria-label="Close navigation"></label>' +
      '<aside class="min-h-full w-72 max-w-[85vw] overflow-y-auto border-r border-base-300 bg-base-200 p-4"></aside>' +
    '</div>';

  var main = drawer.querySelector('main');
  while (document.body.firstChild) main.appendChild(document.body.firstChild);
  document.body.appendChild(drawer);

  /* wide tables scroll horizontally on phones */
  main.querySelectorAll('table').forEach(function (table) {
    var wrap = document.createElement('div');
    wrap.className = 'overflow-x-auto';
    table.parentNode.insertBefore(wrap, table);
    wrap.appendChild(table);
  });

  /* ---------- sidebar ---------- */
  var aside = drawer.querySelector('aside');
  var menu = '';
  groups.forEach(function (g) {
    menu += '<li class="menu-title mt-3"><span>' + g.label + '</span></li>';
    g.items.forEach(function (it) {
      var active = it.file === cur;
      menu += '<li><a href="' + it.href + '"' +
        (active ? ' class="menu-active" aria-current="page"' : '') + '>' +
        it.label + '</a></li>';
    });
  });
  menu += '<li class="menu-title mt-3"><span>Workspace</span></li>' +
    '<li><a href="' + gh + 'README.md" target="_blank" rel="noopener">Readme ↗</a></li>' +
    '<li><a href="' + gh + 'MISSION.md" target="_blank" rel="noopener">Mission ↗</a></li>' +
    '<li><a href="' + gh + 'RESOURCES.md" target="_blank" rel="noopener">Resources ↗</a></li>' +
    '<li><a href="https://github.com/daniel-d7a/ZeroOps" target="_blank" rel="noopener">Source repo ↗</a></li>';

  aside.innerHTML =
    '<div class="px-2 pb-2 pt-1">' +
      '<div class="text-lg font-bold tracking-tight">ZeroOps</div>' +
      '<div class="text-xs opacity-60">architecture field guide</div>' +
    '</div>' +
    '<ul class="menu w-full gap-0.5 p-0 text-sm">' + menu + '</ul>' +
    '<div class="divider my-4"></div>' +
    '<p class="px-2 text-xs leading-relaxed opacity-55">Every diagram names the file and function that implements it. Press <kbd class="kbd kbd-xs">n</kbd> to toggle nav.</p>';

  /* close the mobile drawer after navigating */
  var toggle = document.getElementById('nav-drawer');
  aside.addEventListener('click', function (e) {
    if (e.target.closest('a')) toggle.checked = false;
  });

  /* ---------- controls ---------- */
  document.getElementById('theme-toggle').addEventListener('click', function () {
    applyTheme(html.getAttribute('data-theme') === 'dark' ? 'light' : 'dark');
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') { toggle.checked = false; return; }
    if (e.key !== 'n' || e.metaKey || e.ctrlKey || e.altKey) return;
    var t = e.target && e.target.tagName;
    if (t === 'INPUT' || t === 'TEXTAREA' || t === 'SELECT') return;
    toggle.checked = !toggle.checked;
  });

  applyTheme(html.getAttribute('data-theme') === 'light' ? 'light' : (savedTheme || 'dark'));
})();
