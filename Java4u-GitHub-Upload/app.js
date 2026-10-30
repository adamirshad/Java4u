const dialog = document.querySelector('#info-dialog');
const dialogTitle = document.querySelector('#dialog-title');
const dialogCopy = document.querySelector('#dialog-copy');

function showInfo(title, copy) {
  dialogTitle.innerHTML = `${title}<span class="period">.</span>`;
  dialogCopy.textContent = copy;
  dialog.showModal();
}

document.querySelector('#play-button').addEventListener('click', () => {
  showInfo('Game launch is next', 'This preview shows the direction for Java4u. The next build step is wiring a real instance manager and authenticated Minecraft launch.');
});
document.querySelector('#manage-button').addEventListener('click', () => {
  showInfo('Instance manager', 'Java4u will keep each game setup isolated, with its own version, loader, mods, settings, and saves.');
});
document.querySelector('#new-instance').addEventListener('click', () => {
  showInfo('Create an instance', 'The planned flow will let you pick a Minecraft version, use the Chunky loader or vanilla, and install only the files that instance needs. Fabric mod compatibility is planned for Chunky.');
});
document.querySelector('#all-instances').addEventListener('click', () => {
  showInfo('Your library', 'This preview includes the home screen. The full library will make it quick to find, duplicate, and update instances.');
});
document.querySelectorAll('.nav-item, .release-card').forEach((item) => {
  item.addEventListener('click', (event) => {
    event.preventDefault();
    const label = item.textContent.trim().replace(/\s+/g, ' ');
    showInfo(label, `${label} will be part of Java4u. This preview is focused on the library home screen.`);
  });
});
document.querySelectorAll('.instance-card').forEach((card) => {
  const select = () => {
    document.querySelectorAll('.instance-card').forEach((item) => item.classList.remove('selected'));
    card.classList.add('selected');
  };
  card.addEventListener('click', select);
  card.addEventListener('keydown', (event) => {
    if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); select(); }
  });
});
document.querySelector('.dialog-close').addEventListener('click', () => dialog.close());
document.querySelector('.dialog-ok').addEventListener('click', () => dialog.close());
document.querySelector('.tip-next').addEventListener('click', () => showInfo('One world, one setup', 'Separate instances let you keep a lightweight vanilla world alongside larger modded setups.'));
document.querySelector('.help-button').addEventListener('click', () => showInfo('Java4u preview', 'Java4u is an independent, open-source launcher concept. It is not affiliated with Mojang or Microsoft.'));
