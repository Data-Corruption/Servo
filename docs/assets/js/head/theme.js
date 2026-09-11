// The documentation has a fixed dark palette, independent of dashboard preferences.
function setTheme() {
  document.documentElement.classList.remove('light');
  document.documentElement.classList.add('dark');
  document.documentElement.style.colorScheme = 'dark';
}
setTheme();
