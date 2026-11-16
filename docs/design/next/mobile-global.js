/* Screens outside a server: sign-in, dashboard, Linager updates, audit, settings. */
(()=>{
const {ic,segs,tag,seg,chk,servers}=M;
const LV={ok:'ok',warn:'warn',crit:'crit',off:'off'};
const dotc=l=>l==='ok'?'ok':l==='warn'?'warn':l==='crit'?'crit':'muted';
const flags=s=>[s.upd?tag('обновлений: '+s.upd):'',s.reboot?tag('нужна перезагрузка','warn'):'',s.fail?tag('служба упала','crit'):'',s.lv==='off'?tag('нет связи','muted'):''].join('');
const filters=(cur=0)=>`<div class="stick"><div class="row">${seg(['Все 8','Проблемные 5','Offline 1'],cur).replace('class="seg','class="grow seg')}<button class="ibtn" data-sheet="groups" aria-label="Группа">${ic('folder')}</button></div></div>`;
const summary=`<div class="sum"><div><small>Online</small><b class="crit">7/8</b><span>нет связи: 1</span><i class="mk"></i></div><div><small>Внимание</small><b class="crit">5</b><span>критичных: 1</span><i class="mk"></i></div><div><small>Обновл.</small><b>23</b><span>перезагр.: 1</span></div><div><small>CPU</small><b>42%</b><span>online</span></div></div><div class="live" style="margin:6px 2px 0"><i></i>live · 10:30:40 · Debian · 8 серверов</div>`;

const card=s=>`<button class="card edge ${LV[s.lv]} srv" data-go="srv-overview"><div class="nm"><span class="dot ${dotc(s.lv)}" style="color:var(--${s.lv==='off'?'muted':s.lv==='ok'?'good':s.lv})"></span><span class="grow" style="overflow:hidden;text-overflow:ellipsis">${s.n}</span><span class="ibtn" style="height:36px;width:44px" data-go="srv-terminal" aria-label="Терминал">${ic('terminal')}</span></div>
<div class="meta">${s.ip} · ${s.os} · агент ${s.ag}</div>
${s.lv==='off'?'<div class="muted small" style="padding:6px 0">Последний ответ 12 минут назад</div>':`<div class="met"><div><small>CPU <b>${s.cpu}%</b></small>${segs(s.cpu)}</div><div><small>RAM <b>${s.ram}%</b></small>${segs(s.ram)}</div><div><small>ДИСК <b>${s.dsk}%</b></small>${segs(s.dsk)}</div></div>`}
<div class="ft">${flags(s)}<span style="margin-left:auto">↓ ${s.rx} ↑ ${s.tx}</span></div></button>`;

M.add({id:'login',grp:'Общее',title:'Вход',tabs:null,top:{brand:1},
 was:['Карточка по центру, но поле и кнопки мелкие, при открытой клавиатуре форма уезжает под неё.'],
 notes:['Поля 44 px и шрифт 16 px: iOS не приближает страницу при фокусе.','Форма прижата к верху области, а не по центру: клавиатура её не закрывает.','«Войти» закреплена внизу над клавиатурой, Passkey сверху отдельной кнопкой.','Второй шаг (код 2FA) — тот же экран, поле кода с автозаполнением из SMS/приложения.'],
 body:()=>`<div class="login" style="justify-content:flex-start;padding-top:24px"><h2 class="big">Вход</h2><p class="muted">Панель управления серверами</p>
 <button class="btn blk">${ic('key')} Войти с Passkey</button>
 <div class="muted small" style="text-align:center">или логином и паролем</div>
 <div class="field"><label>Логин</label><input class="input" value="admin" autocapitalize="off"></div>
 <div class="field"><label>Пароль</label><input class="input" type="password" value="••••••••••"></div></div>`,
 dock:`<button class="btn pri blk" data-go="dash">Войти</button>`});

M.add({id:'dash',grp:'Общее',title:'Серверы · карточки',tabs:'g',tab:'dash',
 top:{brand:1,actions:`<button class="ibtn" data-sheet="search" aria-label="Поиск">${ic('search')}</button><button class="ibtn" data-go="dash-map" aria-label="Карта">${ic('map')}</button><button class="ibtn pri" data-sheet="add-server" aria-label="Добавить сервер">${ic('plus')}<i class="dotb"></i></button>`},
 was:['Шапка и четыре плитки занимают первый экран, список серверов начинается ниже сгиба.','Фильтры и поиск переносятся на 3–4 строки; кнопки «Редактировать» и «+ Сервер» под ними.','Меню в шапке раскрывается вниз и сдвигает страницу.'],
 notes:['Сводка — одна компактная строка из четырёх ячеек (≈64 px вместо ≈260 px).','Поиск, вид (карточки/карта) и «+ Сервер» — иконки в шапке; поиск открывается листом.','Фильтр «Все / Проблемные / Offline» и группа — одна закреплённая строка, остаётся на месте при прокрутке.','Карточка целиком — кнопка «открыть сервер»; отдельная «Открыть» убрана. Терминал — иконка справа. «Удалить с дашборда» ушло в режим «Редактировать» (⋯ в «Ещё»).','График CPU за минуту с карточки убран: на телефоне он не читается, он есть на «Обзоре».','Плитка «+ Сервер» с точкой — есть серверы, ждущие подтверждения.','Низ: Серверы · Linager (обновления, счётчик) · Журнал · Ещё (настройки, тема, язык, выход).'],
 body:()=>`<div class="stack">${summary}${filters()}${servers.map(card).join('')}</div>`});

const tile=s=>`<button class="card edge ${LV[s.lv]} tile" data-go="srv-overview"><div class="nm"><span class="dot" style="color:var(--${s.lv==='off'?'muted':s.lv==='ok'?'good':s.lv})"></span>${s.n}</div><div class="ip">${s.ip}</div>${s.lv==='off'?'<div class="muted small">нет связи</div>':[['CPU',s.cpu],['RAM',s.ram],['ДИСК',s.dsk]].map(([l,v])=>`<div class="m"><span class="muted">${l}</span>${segs(v,10)}<b class="${M.lvl(v)}">${v}%</b></div>`).join('')}<div class="muted" style="font-size:10.5px;margin-top:5px">↓${s.rx} ↑${s.tx}</div></button>`;
M.add({id:'dash-map',grp:'Общее',title:'Серверы · карта',tabs:'g',tab:'dash',
 top:{brand:1,actions:`<button class="ibtn" data-sheet="search">${ic('search')}</button><button class="ibtn" data-go="dash" aria-label="Карточки">${ic('cards')}</button><button class="ibtn pri" data-sheet="add-server">${ic('plus')}</button>`},
 notes:['Две плитки в ряд, всё помещается без горизонтальной прокрутки: 8 серверов на ~2,5 экрана.','Иконка вида в шапке показывает, на какой вид переключит.'],
 body:()=>`<div class="stack">${summary}${filters()}<div class="tiles">${servers.map(tile).join('')}</div></div>`});

M.add({id:'dash-edit',grp:'Общее',title:'Серверы · редактирование',tabs:null,
 top:{title:'Редактирование',sub:'порядок и состав дашборда',actions:`<button class="btn sm pri" data-go="dash">Готово</button>`},
 was:['Перетаскивание карточек и кнопки ‹ ›. Перетаскивание на телефоне конфликтует с прокруткой.'],
 notes:['Без перетаскивания: кнопки ▲ ▼ на каждой строке, строки компактные — весь дашборд виден за 1–2 экрана.','Удаление с дашборда — «×» с отменой 5 секунд (плашка снизу). Сам сервер остаётся подключённым.'],
 body:()=>`<div class="list">${servers.map((s,i)=>`<div class="li"><span class="dot" style="color:var(--${s.lv==='off'?'muted':s.lv==='ok'?'good':s.lv})"></span><div class="mn"><div class="l1"><b>${s.n}</b></div><div class="l2">${s.ip}</div></div><button class="ibtn" ${i?'':'disabled style="opacity:.35"'}>${ic('up')}</button><button class="ibtn" style="transform:none"><span style="display:block;transform:rotate(180deg)">${ic('up')}</span></button><button class="ibtn" style="color:var(--crit)">${ic('x')}</button></div>`).join('')}</div>`,
 dock:`<div class="t muted">Удалён «backup-01» с дашборда. <b style="color:var(--accent)">Отменить (4 с)</b></div>`});

M.add({id:'updates',grp:'Общее',title:'Обновления Linager',tabs:'g',tab:'updates',
 top:{title:'Обновления',sub:'Linager · сервер и агенты'},
 was:['Широкие таблицы (агенты, кэш, история) с горизонтальной прокруткой.'],
 notes:['Таблицы превращены в строки: сервер · версия · статус в две строки, ход обновления — полоса под строкой.','«Обновить до 0.1.52» закреплена внизу; подтверждение — лист с кратким перечнем шагов.','Блок «То же из терминала» свёрнут, копирование команды — тапом по строке.'],
 body:()=>`<div class="stack">
 <div class="card"><div class="row sp"><h3 class="t">Сервер</h3>${tag('есть обновление','warn')}</div>
 <div class="kv"><span>Версия</span><span>0.1.51</span></div><div class="kv"><span>Доступна</span><span class="acc">0.1.52</span></div><div class="kv"><span>Канал</span><span>stable</span></div><div class="kv"><span>Миграции БД</span><span>актуальны</span></div><div class="kv"><span>Бэкап БД</span><span>сегодня 03:00</span></div>
 <button class="btn blk sm" style="margin-top:8px">Проверить обновления</button></div>
 <div class="sech"><h3 class="t">Агенты</h3><span class="muted small">7 из 8</span></div>
 <div class="list">${servers.slice(0,6).map((s,i)=>`<div class="li" style="flex-wrap:wrap"><div class="mn"><div class="l1"><b>${s.n}</b></div><div class="l2">версия ${s.ag}</div></div>${i===1?tag('обновляется 3/5','warn'):i===4?tag('доступна 0.3.2','warn'):tag('актуален','ok')}${i===1?`<div class="pbar" style="width:100%"><i style="width:60%"></i></div>`:''}</div>`).join('')}</div>
 <div class="sech"><h3 class="t">Кэш релизов</h3><button class="btn sm">Удалить старые (2)</button></div>
 <div class="list"><div class="li"><div class="mn"><div class="l1"><b>0.1.51</b>${tag('работает','ok')}</div><div class="l2">подпись проверена</div></div></div><div class="li"><div class="mn"><div class="l1"><b>0.1.50</b></div><div class="l2">подпись проверена</div></div></div></div>
 <div class="sech"><h3 class="t">История</h3></div>
 <div class="list"><div class="li"><div class="mn"><div class="l1"><b>0.1.50 → 0.1.51</b>${tag('успешно','ok')}</div><div class="l2">14 нояб · admin</div></div></div><div class="li"><div class="mn"><div class="l1"><b>0.1.49 → 0.1.50</b>${tag('успешно','ok')}</div><div class="l2">9 нояб · admin</div></div></div></div></div>`,
 dock:`<button class="btn pri blk" data-sheet="confirm-update">Обновить до 0.1.52</button>`});

M.add({id:'audit',grp:'Общее',title:'Журнал действий',tabs:'g',tab:'audit',
 top:{title:'Журнал',sub:'действия администраторов',actions:`<button class="ibtn" data-sheet="audit-filter">${ic('search')}</button>`},
 notes:['Каждая запись — две строки: действие и «кто · где · когда». Подробности (адрес, параметры) раскрываются тапом.','Фильтры (пользователь, сервер, тип, период) — лист; активные показаны чипсами под шапкой.','Подгрузка «Показать ещё» кнопкой, без бесконечной прокрутки.'],
 body:()=>`<div class="stack"><div class="row wrap">${tag('Все пользователи','')}${tag('7 дней','')}</div><div class="list">${[
 ['Перезагрузка сервера','admin · web-02','10:12','warn'],['Файрвол: применены правила','admin · web-01','09:48',''],['Служба nginx перезапущена','ops · web-01','09:31',''],['Установка обновлений (безопасность)','admin · mail-01','вчера 22:10',''],['Вход с Passkey','admin · 10.0.0.5','вчера 21:58',''],['Пользователь deploy создан','admin · ci-01','вчера 18:02',''],['Контейнер redis удалён','ops · cache-01','13 нояб','crit']].map(r=>`<button class="li ${r[3]==='crit'?'bad':r[3]==='warn'?'wn':''}"><div class="mn"><div class="l1"><b>${r[0]}</b></div><div class="l2">${r[1]} · ${r[2]}</div></div>${ic('next','s')}</button>`).join('')}</div><button class="btn blk">Показать ещё</button></div>`});

const grow=(ico,t,s,go)=>`<button class="li" ${go?`data-go="${go}"`:''}>${ic(ico)}<div class="mn"><div class="l1"><b>${t}</b></div>${s?`<div class="l2">${s}</div>`:''}</div>${ic('next','s')}</button>`;
M.add({id:'settings',grp:'Общее',title:'Ещё',tabs:'g',tab:'settings',
 top:{title:'Ещё',sub:'admin · администратор'},
 was:['Окно «Настройки» с боковой панелью, на телефоне — полоса разделов; тема, язык и «Выйти» спрятаны в меню шапки.'],
 notes:['Один список: настройки, быстрые переключатели темы и языка, выход. Каждый раздел — отдельный экран с «‹ Назад» (вместо окна в окне).','Опасное («Выйти») внизу и с подтверждением.'],
 body:()=>`<div class="stack"><div class="list">${grow('users','Профиль и безопасность','пароль, 2FA, Passkey, коды восстановления','settings-profile')}${grow('users','Пользователи','3 учётные записи, роли')}${grow('terminal','Сниппеты терминала','общие, 6 штук')}${grow('updates','Обновления Linager','канал stable, автопроверка')}${grow('audit','Журнал действий','','audit')}</div>
 <div class="sech"><h3 class="t">Оформление</h3></div>
 <div class="card stack" style="gap:10px"><div class="field"><label>Тема</label>${seg(['Системная','Светлая','Тёмная'],0)}</div><div class="field"><label>Язык</label>${seg(['Русский','English'],0)}</div></div>
 <div class="sech"><h3 class="t">Дашборд</h3></div>
 <div class="list">${grow('cards','Редактировать дашборд','порядок и состав','dash-edit')}${grow('folder','Группы серверов','4 группы')}</div>
 <button class="btn blk dng" style="margin-top:4px">Выйти</button><p class="muted small" style="text-align:center">Linager 0.1.51 · Apache-2.0</p></div>`});

M.add({id:'settings-profile',grp:'Общее',title:'Профиль',tabs:null,
 top:{title:'Профиль',sub:'admin',back:'settings'},
 notes:['Каждая группа настроек — карточка; кнопки на всю ширину, без «двух рядом».','Сохранить — закреплённая кнопка внизу, активна только при изменениях.'],
 body:()=>`<div class="stack"><div class="card stack"><h3 class="t">Данные</h3><div class="field"><label>Имя</label><input class="input" value="Администратор"></div><div class="field"><label>Почта</label><input class="input" value="admin@example.org"></div></div>
 <div class="card stack"><h3 class="t">Двухфакторная защита</h3><div class="row sp"><span>Приложение-аутентификатор</span>${tag('включена','ok')}</div><div class="row sp"><span>Passkey</span>${tag('2 ключа','ok')}</div><button class="btn blk">Коды восстановления</button><button class="btn blk">Добавить Passkey</button></div>
 <div class="card stack"><h3 class="t">Пароль</h3><button class="btn blk">Сменить пароль</button></div></div>`,
 dock:`<button class="btn pri blk" disabled>Сохранить</button>`});

/* sheets */
M.sheet({id:'search',title:'Поиск',under:'dash',body:()=>`<div class="field"><label>Имя или адрес</label><input class="input" placeholder="web-01, 10.0.1.…" value="web"></div><div class="list" style="margin-top:12px">${servers.slice(0,2).map(s=>`<button class="li" data-go="srv-overview"><span class="dot" style="color:var(--good)"></span><div class="mn"><div class="l1"><b>${s.n}</b></div><div class="l2">${s.ip}</div></div></button>`).join('')}</div>`});
M.sheet({id:'groups',title:'Группа',under:'dash',flush:1,body:()=>['Все группы','Без группы','Продакшн','Тест','Домашняя сеть'].map((g,i)=>`<button class="act">${g}${i===0?`<small>${ic('chev','s')}</small>`:''}</button>`).join('')+`<button class="act">${ic('plus','s')} Группы…</button>`,notes:[]});
M.sheet({id:'add-server',title:'Добавить сервер',under:'dash',full:1,
 body:()=>`<h3 class="t">Ждут подтверждения</h3><div class="card stack" style="margin:8px 0 14px"><div><b style="font-family:var(--font-display);font-size:18px">cache-02</b><div class="muted small">10.0.1.23 · отпечаток 4F:A1:…:9C</div></div><div class="field"><label>Ключ с сервера</label><input class="input" placeholder="XXXX-XXXX" value="7QK2-M4PD"></div><button class="btn pri blk">Подтвердить</button></div>
 <h3 class="t">Подключённые, нет на дашборде</h3><div class="list" style="margin-top:8px"><div class="li"><div class="mn"><div class="l1"><b>backup-01</b></div><div class="l2">10.0.3.9</div></div><button class="btn sm">Добавить</button></div></div>
 <h3 class="t" style="margin-top:14px">Новый сервер</h3><p class="muted small" style="margin-top:6px">Команда установки агента — тап по строке копирует её.</p><div class="term" style="margin-top:6px">curl -fsSL https://panel/install.sh | sh</div>`,
 foot:`<button class="btn f1" data-close>Закрыть</button>`});
M.sheet({id:'confirm-update',title:'Обновить Linager?',under:'updates',body:()=>`<p>Сервер обновится с 0.1.51 до <b>0.1.52</b>. Панель будет недоступна около минуты. Перед обновлением делается копия базы.</p><ul class="small muted" style="padding-left:18px"><li>проверка подписи</li><li>pg_dump</li><li>остановка и замена бинарника</li><li>миграции и запуск</li></ul>`,foot:`<button class="btn f1" data-close>Отмена</button><button class="btn pri f1">Обновить</button>`});
M.sheet({id:'audit-filter',title:'Фильтры журнала',under:'audit',body:()=>`<div class="stack"><div class="field"><label>Пользователь</label><button class="sel"><span>Все</span>${ic('chev','s')}</button></div><div class="field"><label>Сервер</label><button class="sel"><span>Все</span>${ic('chev','s')}</button></div><div class="field"><label>Период</label>${seg(['1д','7д','30д','Все'],1)}</div><div class="field"><label>Тип</label><button class="sel"><span>Все действия</span>${ic('chev','s')}</button></div></div>`,foot:`<button class="btn f1">Сбросить</button><button class="btn pri f1" data-close>Показать</button>`});
})();
