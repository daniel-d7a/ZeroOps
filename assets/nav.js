// Collapsible sidebar navigation, injected on every lesson/reference page.
(function () {
  var groups = [
    { label: 'Lessons', base: '../lessons/', items: [
      ['0001-architecture-30000ft.html', '1 · The 30,000-ft tour'],
      ['0002-finite-state-machine.html', '2 · Finite state machine'],
    ]},
    { label: 'Reference', base: '../reference/', items: [
      ['architecture.html', 'Architecture map'],
      ['deploy-lifecycle.html', 'Deploy lifecycle'],
      ['queue-topology.html', 'Queue topology'],
      ['api-map.html', 'API map'],
      ['worker-map.html', 'Worker map'],
      ['glossary.html', 'Glossary'],
    ]},
  ];
  var cur = decodeURIComponent(location.pathname.split('/').pop());
  if (!cur || !/\.html$/.test(cur)) cur = 'architecture.html';

  var aside = document.createElement('aside');
  aside.id = 'sidebar';
  aside.innerHTML =
    '<div class="nav-head">ZeroOps<span class="sub">teaching workspace</span></div>';
  var nav = document.createElement('nav');
  groups.forEach(function (g) {
    var gh = document.createElement('div');
    gh.className = 'nav-group';
    gh.textContent = g.label;
    nav.appendChild(gh);
    g.items.forEach(function (it) {
      var a = document.createElement('a');
      a.href = g.base + it[0];
      a.textContent = it[1];
      a.className = 'nav-link';
      if (it[0] === cur) {
        a.classList.add('active');
        a.setAttribute('aria-current', 'page');
      }
      nav.appendChild(a);
    });
  });
  nav.appendChild(document.createElement('div')).className = 'nav-group';
  nav.lastChild.style.marginTop = '2rem';
  nav.lastChild.textContent = 'Workspace';
  ['../README.md', '../MISSION.md', '../RESOURCES.md'].forEach(function (p) {
    var name = p.split('/').pop().replace('.md', '');
    var a = document.createElement('a');
    a.href = p;
    a.textContent = name.charAt(0).toUpperCase() + name.slice(1);
    a.className = 'nav-link';
    nav.appendChild(a);
  });
  aside.appendChild(nav);

  var foot = document.createElement('div');
  foot.className = 'nav-foot';
  foot.innerHTML = 'Everything links straight to real files —';
  var repo = document.createElement('a');
  repo.href = 'https://github.com/daniel-d7a/ZeroOps';
  repo.textContent = 'source repo';
  repo.target = '_blank';
  repo.rel = 'noopener';
  foot.appendChild(document.createElement('br'));
  foot.appendChild(repo);
  aside.appendChild(foot);

  document.body.insertBefore(aside, document.body.firstChild);

  var btn = document.createElement('button');
  btn.id = 'navToggle';
  btn.type = 'button';
  btn.innerHTML = '&#9776;';
  btn.title = 'Toggle navigation (n)';
  document.body.appendChild(btn);

  var KEY = 'zeroops.nav.collapsed';
  function apply(collapsed) {
    document.body.classList.toggle('nav-collapsed', collapsed);
    try { localStorage.setItem(KEY, collapsed ? '1' : '0'); } catch (e) {}
  }
  btn.onclick = function () {
    apply(!document.body.classList.contains('nav-collapsed'));
  };
  document.addEventListener('keydown', function (e) {
    if (e.key !== 'n' || e.metaKey || e.ctrlKey || e.altKey) return;
    var t = e.target && e.target.tagName;
    if (t === 'INPUT' || t === 'TEXTAREA' || t === 'SELECT') return;
    apply(!document.body.classList.contains('nav-collapsed'));
  });
  var saved = null;
  try { saved = localStorage.getItem(KEY); } catch (e) {}
  if (window.innerWidth < 900 || saved === '1') apply(true);
})();
