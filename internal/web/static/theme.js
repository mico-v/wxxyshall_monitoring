// 跟随仪表盘保存的浅/深色偏好（同一 localStorage 键）；无偏好时跟随系统。
// 在应用脚本执行前切换 <html> 的 dark 类，避免首屏闪烁。
try {
  var t = localStorage.getItem("elec-theme");
  var dark = t ? t === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
  if (dark) document.documentElement.classList.add("dark");
} catch (e) {}
