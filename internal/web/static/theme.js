// 跟随仪表盘保存的浅/深色主题(同一 localStorage 键)
try {
  var t = localStorage.getItem("elec-theme");
  if (t) document.documentElement.setAttribute("data-theme", t);
} catch (e) {}
