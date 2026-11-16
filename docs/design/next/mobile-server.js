/* Server page: overview, metrics, terminal, files, services, processes, logs, updates. */
(()=>{
const {ic,segs,tag,seg,chk,chart}=M;
const st=(tab,extra='',sub)=>({title:M.TAB_NAMES[tab],sub:sub||'<span class="ok">■</span> web-01 · 10.0.1.11 · в норме',back:'dash',actions:extra+`<button class="ibtn" data-sheet="srv-menu" aria-label="Действия сервера">${ic('more')}</button>`});
M.st=st;
const base={grp:'Сервер web-01',tabs:'s'};
const sec=(t,r='')=>`<div class="sech"><h3 class="t">${t}</h3>${r}</div>`;
const row=(l1,l2,r,cls='',go='')=>`<button class="li ${cls}" ${go}><div class="mn"><div class="l1">${l1}</div>${l2?`<div class="l2">${l2}</div>`:''}</div>${r?`<div class="r">${r}</div>`:''}</button>`;

M.add({...base,id:'srv-overview',title:'Обзор',tab:'srv-overview',top:st('overview'),
 was:['Шапка в два ряда («‹ Серверы», имя, статус, «Перезагрузить»), два баннера, под ними выпадающее меню разделов, которое раскрывается вниз и сдвигает страницу; панель в рамке внутри страницы.'],
 notes:['Шапка одна: ‹ назад, раздел крупно, под ним имя, адрес и статус сервера. «Перезагрузить» и «Обновить агент» — в меню ⋯ (с подтверждением).','Баннеры «нужна перезагрузка» и «агент устарел» — одна строка-кнопка каждый, тап открывает лист с действием.','Разделы — нижняя панель (Обзор · Терминал · Файлы · Службы) и «Разделы» со всем списком по группам. Лист поверх страницы, ничего не сдвигает.','Плитки CPU/RAM/сеть по две в ряд, диски и интерфейсы — полной шириной. Лишней внешней рамки панели нет: экономим по 20 px с каждой стороны.','«Система» — свёрнутый список; раскрывается на месте, но под ним ничего важного.'],
 body:()=>`<div class="stack">
 <button class="warnbox" data-sheet="reboot"><span>Нужна перезагрузка: ядро обновлено</span>${ic('next','s')}</button>
 <button class="warnbox" data-sheet="agent"><span>Доступна новая версия агента 0.3.3</span>${ic('next','s')}</button>
 <div class="tiles">
  <div class="card"><h3 class="t">CPU</h3><div class="big">36%</div>${segs(36,20,'t')}<div class="muted small" style="margin-top:6px">load 1.45 · 1.36 · 1.26</div></div>
  <div class="card"><h3 class="t">RAM</h3><div class="big">52%</div>${segs(52,20,'t')}<div class="muted small" style="margin-top:6px">4.0 из 8 GiB · swap 2</div></div>
  <div class="card"><h3 class="t">Приём</h3><div class="big" style="font-size:22px">892 KB/s</div>${chart(3,'var(--series-1)',{h:60,lo:30,hi:70,area:false}).replace(/<text[^>]*>[^<]*<\/text>/g,'')}</div>
  <div class="card"><h3 class="t">Отдача</h3><div class="big" style="font-size:22px">715 KB/s</div>${chart(7,'var(--series-2)',{h:60,lo:30,hi:70,area:false}).replace(/<text[^>]*>[^<]*<\/text>/g,'')}</div>
 </div>
 <div class="card"><h3 class="t">Диски</h3>${[['/',41],['/var',27],['/home',17]].map(([n,v])=>`<div class="row" style="margin-top:8px"><span style="width:56px">${n}</span><div class="grow">${segs(v,20)}</div><b style="font-weight:400;width:40px;text-align:right">${v}%</b></div>`).join('')}</div>
 ${sec('Интерфейсы')}<div class="list">${row('<b>eth0</b>','10.0.1.11','↓ 892 KB/s<br>↑ 715 KB/s')}${row('<b>lo</b>','127.0.0.1','↓ 26 KB/s<br>↑ 28 KB/s')}</div>
 ${sec('Система')}<div class="card">${[['ОС','Debian 12'],['Ядро','6.1.0-27-amd64'],['Адрес','10.0.1.11'],['Процессор','4 vCPU'],['Uptime','41д 6ч'],['Агент','linager 0.3.2'],['Контакт','2 с назад']].map(([a,b])=>`<div class="kv"><span>${a}</span><span>${b}</span></div>`).join('')}</div></div>`});

M.add({...base,id:'srv-metrics',title:'Метрики',tab:'srv-metrics',top:st('metrics'),
 was:['Период — выпадающий список или ряд кнопок, четыре графика с длинной подписью, события в блоке с внутренней прокруткой (вложенный скролл).'],
 notes:['Период — один сегмент на всю ширину (семь значений помещаются). Закреплён под шапкой: график листается, период виден.','Графики на всю ширину, под каждым — мин / средн / p95 / пик четырьмя ячейками. Подсказка — по тапу и движению пальца по графику (не свайп страницы: касание графика только показывает значения).','«События за период» — раскрывающийся блок на месте, без вложенной прокрутки; показывает 5 записей и «Показать все».','«Синхронизировать» — переключатель, поведение прежнее.'],
 body:()=>`<div class="stack"><div class="stick">${seg(['1ч','24ч','7д','30д','90д','180д','360д'],1,'sm')}</div>
 ${[['CPU, %','var(--series-1)',11],['RAM, %','var(--series-1)',5],['Сеть, KB/s','var(--series-1)',9],['Диск, %','var(--series-1)',2]].map(([t,c,s],i)=>`<div class="card"><div class="row sp"><h3 class="t">${t}</h3>${i===2?'<span class="small"><span class="acc">■</span> приём <span style="color:var(--series-2)">■</span> отдача</span>':'<span class="small muted">среднее · пик</span>'}</div>${chart(s,c,{h:128,two:i===2})}<div class="met" style="grid-template-columns:repeat(4,1fr);gap:6px;margin-top:6px">${['мин','средн','p95','пик'].map((l,j)=>`<div><small>${l}</small><b style="font-size:15px;font-weight:400">${[4,36,71,93][j]}%</b></div>`).join('')}</div></div>`).join('')}
 ${M.chk('Синхронизировать наведение графиков',1)}
 ${sec('События за период','<span class="muted small">12</span>')}<div class="list">${[['Перезагрузка','вчера 03:12','crit'],['Обновление агента','12 нояб',''],['Служба nginx упала','11 нояб','wn']].map(r=>row('<b>'+r[0]+'</b>',r[1],'',r[2])).join('')}</div><button class="btn blk sm">Показать все</button></div>`});

M.add({...base,id:'srv-terminal',title:'Терминал',tab:'srv-terminal',
 top:st('terminal',`<button class="ibtn" data-sheet="snippets" aria-label="Сниппеты">${ic('bolt')}</button>`,'<span class="ok">■</span> SSH через агент · root'),
 was:['Область терминала ограничена высотой блока с рамкой и заголовком, под ней подсказка; клавиатура iOS закрывает половину, панель клавиш отдельным окном.'],
 notes:['Терминал занимает всё, что есть между шапкой и панелью клавиш; рамки и подсказок вокруг нет. Высота пересчитывается по visualViewport, когда появляется клавиатура.','Над клавиатурой — один ряд служебных клавиш: Esc, Tab, Ctrl (залипает), Alt, стрелки; «Ещё» открывает расширенную панель (F-клавиши, Home/End, PgUp/PgDn, Ctrl-комбинации).','Сниппеты — иконка в шапке (лист со списком: тап вставляет и выполняет, «→» только вставляет). Пользователь, от которого запущена оболочка — «root ▾» в подзаголовке.','Состояние «клавиатура открыта»: нижняя панель разделов скрывается, остаётся ряд клавиш.'],
 scrollCls:'flush',body:()=>`<div class="term"><span class="p">root@web-01:~#</span> df -h
Filesystem      Size  Used Avail Use% Mounted on
/dev/vda1        40G   16G   25G  41% /
/dev/vdb1        40G   11G   29G  27% /var
/dev/vdc1       120G   20G  100G  17% /home
tmpfs           3.9G     0  3.9G   0% /dev/shm
<span class="p">root@web-01:~#</span> systemctl status nginx
● nginx.service - A high performance web server
     Active: <span style="color:#4fcf86">active (running)</span> since Mon 2026-11-02
   Main PID: 612 (nginx)
      Tasks: 5 (limit: 4575)
<span class="p">root@web-01:~#</span> <span style="background:#d8dde6;color:#0b0e13">&nbsp;</span></div>`});
M.add({...base,id:'srv-terminal-kbd',title:'Терминал · клавиатура',tab:'srv-terminal',tabs:null,kbd:1,scrollCls:'flush',
 top:st('terminal',`<button class="ibtn" data-sheet="snippets">${ic('bolt')}</button>`,'<span class="ok">■</span> SSH через агент · root'),
 notes:['Клавиатура открыта: панель разделов скрыта, у терминала больше строк. Ctrl подсвечен как включённый.'],
 body:()=>M.screens['srv-terminal'].body()});

const fileRows=[['folder','html','папка · 12 июн','', 'f'],['folder','logs','папка · сегодня','', 'f'],['folder','conf.d','папка · 3 нояб','', 'f'],['file','nginx.conf','1.9 KB · 3 нояб · root','', ''],['file','mime.types','5.3 KB · 12 июн','', ''],['file','fastcgi_params','1.0 KB · 12 июн','', '']];
M.add({...base,id:'srv-files',title:'Файлы',tab:'srv-files',top:st('files',`<button class="ibtn" data-sheet="file-new" aria-label="Создать">${ic('plus')}</button>`),
 was:['Дерево папок слева и содержимое справа: на телефоне дерево сверху, потом таблица — два экрана прокрутки до файла; действия над файлом — правый клик.'],
 notes:['Один столбец: путь (хлебные крошки, начало обрезается «…», тап по сегменту — переход) и список. Дерево — лист «Папки» (кнопка слева).','Строка файла: иконка, имя, размер · дата · владелец. Тап по папке заходит, по файлу открывает действия (лист: Открыть в редакторе, Скачать, Права и владелец, Переименовать, Копировать, Удалить). Правого клика и долгого нажатия не требуется.','«Выбрать» включает режим выбора с чекбоксами и нижней панелью (Копировать, Переместить, Удалить, Права).','Загрузка файла — «+» → «Загрузить» (системный выбор файла iOS).'],
 body:()=>`<div class="stack"><div class="row"><button class="btn sm" data-sheet="tree">${ic('folder','s')} Папки</button><button class="btn sm">${ic('search','s')}</button><button class="btn sm grow">Выбрать</button></div>
 <div class="row small" style="overflow:hidden;white-space:nowrap"><span class="muted">…</span><span>/</span><span>etc</span><span>/</span><b class="acc">nginx</b></div>
 <div class="list"><button class="li">${ic('back')}<div class="mn"><div class="l1"><b>..</b></div></div></button>${fileRows.map(r=>`<button class="li" ${r[4]?'':'data-sheet="file-act"'}>${ic(r[0])}<div class="mn"><div class="l1"><b>${r[1]}</b></div><div class="l2">${r[2]}</div></div>${ic(r[4]?'next':'more','s')}</button>`).join('')}</div></div>`});

const svc=[['nginx','HTTP-сервер nginx','работает','ok'],['fail2ban','Защита от перебора паролей','работает','ok'],['ssh','OpenBSD Secure Shell server','работает','ok'],['cron','Планировщик задач cron','работает','ok'],['redis-server','Redis','ошибка','crit'],['systemd-timesyncd','Синхронизация времени','работает','ok'],['ufw','Файрвол ufw','остановлена','muted'],['linager-agent','Агент Linager','работает','ok']];
M.add({...base,id:'srv-services',title:'Службы',tab:'srv-services',top:st('services'),
 was:['Столбцы «Служба / Состояние / Автозапуск / Действия» с двумя кнопками на строку; на телефоне строка высотой в экран.'],
 notes:['Строка: имя и описание слева, состояние справа, «⋯» — лист действий (Запустить/Остановить, Перезапустить, автозапуск, Игнорировать, Логи службы). Опасные действия просят подтверждения.','У упавшей службы под строкой одна быстрая кнопка «Перезапустить» — частый случай не требует листа.','Поиск и фильтр «Все / Упавшие» закреплены под шапкой.'],
 body:()=>`<div class="stack"><div class="stick"><div class="stack" style="gap:8px"><input class="input" placeholder="Поиск службы"><div class="row">${seg(['Все 8','Работают 6','Упавшие 1'],0,'sm').replace('class="seg','class="grow seg')}</div></div></div>
 <div class="list">${svc.map(r=>`<div class="li ${r[3]==='crit'?'bad':''}" style="flex-wrap:wrap"><div class="mn"><div class="l1"><b>${r[0]}</b></div><div class="l2">${r[1]}</div></div>${tag(r[2],r[3])}<button class="ibtn" style="width:40px;height:40px" data-sheet="svc-act">${ic('more')}</button>${r[3]==='crit'?'<button class="btn sm blk" style="margin:2px 0 0">Перезапустить</button>':''}</div>`).join('')}</div></div>`});

M.add({...base,id:'srv-processes',title:'Процессы',tab:'srv-processes',top:st('processes'),
 was:['Таблица с PID, именем, CPU, RAM, кнопкой «Завершить» — на узком экране прокручивается вбок.'],
 notes:['Сегмент «CPU / RAM» сортирует топ-10; строка: имя, пользователь·PID, значение и полоса. «Завершить» — в листе (SIGTERM / SIGKILL) с подтверждением.','Обновление раз в 2 с не сдвигает строки под пальцем: порядок «замораживается», пока лист открыт.'],
 body:()=>`<div class="stack">${seg(['По CPU','По RAM'],0)}<div class="list">${[['mysqld',1021,'mysql',62],['php-fpm: pool www',3320,'www-data',31],['nginx: worker',612,'www-data',9],['redis-server',845,'redis',6],['node',2210,'deploy',5],['systemd-journald',301,'root',2],['sshd',911,'root',1],['cron',455,'root',0]].map(r=>`<button class="li" data-sheet="proc-act"><div class="mn"><div class="l1"><b>${r[0]}</b></div><div class="l2">${r[2]} · PID ${r[1]}</div></div><div style="width:92px"><div class="r" style="margin-bottom:3px">${r[3]}%</div>${segs(r[3],10)}</div></button>`).join('')}</div></div>`});

const logs=[['10:30:12','nginx','GET /api/status 200 0.012s'],['10:30:11','sshd','Accepted publickey for root from 10.0.0.5'],['10:30:09','kernel','nft: IN=eth0 SRC=185.220.101.4 DPT=22 DROP'],['10:30:05','cron','(root) CMD (/usr/local/bin/backup.sh)'],['10:29:58','redis','Background saving started by pid 4421'],['10:29:50','nginx','upstream timed out (110) while reading']];
M.add({...base,id:'srv-logs',title:'Логи',tab:'srv-logs',top:st('logs',`<button class="ibtn" aria-label="Пауза">${ic('stop')}</button>`),
 was:['Журнал в блоке с фиксированной высотой и своей прокруткой внутри прокручиваемой страницы.'],
 notes:['Журнал — единственный прокручиваемый элемент экрана и занимает всю высоту; строки переносятся, а не уходят вбок.','Источник и фильтр — одна закреплённая строка. Пауза — кнопка в шапке; при прокрутке вверх включается пауза автоматически, внизу всплывает «↓ к концу».'],
 body:()=>`<div class="stack"><div class="stick"><div class="row"><button class="sel grow" data-sheet="log-src"><span>Все источники</span>${ic('chev','s')}</button><button class="ibtn">${ic('search')}</button></div></div><div class="term" style="border-color:var(--line);font-size:11.5px">${logs.map(l=>`<span style="color:#8a93a4">${l[0]}</span> <span class="p">${l[1]}</span> ${l[2]}\n`).join('')}</div></div>`});

M.add({...base,id:'srv-updates',title:'Обновления',tab:'srv-updates',top:st('updates'),
 was:['Список пакетов в таблице, кнопки «Проверить», «Только безопасность», «Установить всё» в шапке панели.'],
 notes:['Итог вверху: 12 пакетов, из них 3 безопасности; «Проверить» — маленькая кнопка в карточке.','Установка — закреплённые внизу кнопки «Безопасность (3)» и «Всё (12)»; обе с листом подтверждения. Ход установки — полноэкранный лист с полосой и по-пакетным списком.','Строка пакета: имя, «старая → новая версия», метки «безопасность»/«перезагрузка».'],
 body:()=>`<div class="stack"><div class="card"><div class="row sp"><div><div class="big">12</div><div class="muted small">пакетов · 3 безопасности</div></div><button class="btn sm">${ic('refresh','s')} Проверить</button></div><div class="muted small" style="margin-top:6px">Проверено сегодня в 09:00</div></div>${seg(['Все 12','Безопасность 3'],0)}
 <div class="list">${[['openssl','3.0.15 → 3.0.16','безопасность'],['libssl3','3.0.15 → 3.0.16','безопасность'],['linux-image-6.1','6.1.0-27 → 6.1.0-28','перезагрузка'],['nginx','1.22.1-9 → 1.22.1-10',''],['curl','7.88.1 → 7.88.2','безопасность'],['tzdata','2025b → 2025c','']].map(r=>`<div class="li"><div class="mn"><div class="l1"><b>${r[0]}</b></div><div class="l2">${r[1]}</div></div>${r[2]?tag(r[2],r[2]==='безопасность'?'crit':'warn'):''}</div>`).join('')}</div></div>`,
 dock:`<div class="row"><button class="btn f1" data-sheet="apt-progress">Безопасность (3)</button><button class="btn pri f1" data-sheet="apt-progress">Всё (12)</button></div>`});

/* sheets */
M.sheet({id:'sections',title:'Разделы',under:'srv-docker',flush:1,body:()=>M.SRV_TABS.map(([id,l,tabs])=>`<div class="grp">${l}</div><div class="navgrid">${tabs.map(t=>`<button class="navit" aria-current="${t==='docker'}" data-go="${['overview','terminal','files','services'].includes(t)?'srv-'+t:'srv-'+t}">${ic(t)}<span>${M.TAB_NAMES[t]}</span>${M.TAB_BADGE[t]?`<em class="cnt" style="font-style:normal;${M.TAB_BADGE[t][1]==='crit'?'background:var(--crit)':''}">${M.TAB_BADGE[t][0]}</em>`:''}</button>`).join('')}</div>`).join('')});
M.sheet({id:'srv-menu',title:'web-01',under:'srv-overview',flush:1,body:()=>`<button class="act" data-sheet="reboot">${ic('refresh')} Перезагрузить сервер</button><button class="act" data-sheet="agent">${ic('updates')} Обновить агент<small>0.3.2 → 0.3.3</small></button><button class="act">${ic('terminal')} Открыть терминал</button><button class="act dng">${ic('trash')} Убрать с дашборда</button>`});
M.sheet({id:'reboot',title:'Перезагрузить сервер?',under:'srv-overview',body:()=>`<p>web-01 (10.0.1.11) перезагрузится. Соединения с ним, в том числе терминал, оборвутся примерно на минуту. Действие попадёт в журнал.</p>`,foot:`<button class="btn f1" data-close>Отмена</button><button class="btn dng f1">Перезагрузить</button>`});
M.sheet({id:'agent',title:'Обновить агент',under:'srv-overview',body:()=>`<p>Агент 0.3.2 будет заменён на 0.3.3, служба linager-agent перезапустится. Сервер остаётся доступным.</p>`,foot:`<button class="btn f1" data-close>Отмена</button><button class="btn pri f1">Обновить</button>`});
M.sheet({id:'snippets',title:'Сниппеты',under:'srv-terminal',flush:1,body:()=>['Обновить пакеты','Место на дисках','Последние ошибки журнала','Перезапустить nginx','Кто залогинен'].map(t=>`<div class="act" style="padding-right:0"><span class="grow">${t}</span><button class="ibtn flat" aria-label="Только вставить">${ic('back')}</button></div>`).join('')+'<button class="act">Настроить…</button>',foot:null});
M.sheet({id:'tree',title:'Папки',under:'srv-files',body:()=>`<div class="list">${[['/',0],['etc',1],['nginx',2],['var',1],['home',1],['usr',1]].map(([n,d])=>`<button class="li" style="min-height:46px;padding-left:${12+d*18}px">${ic('folder','s')}<b style="font-weight:400" class="${n==='nginx'?'acc':''}">${n}</b></button>`).join('')}</div>`});
M.sheet({id:'file-act',title:'nginx.conf',under:'srv-files',flush:1,body:()=>[['file','Открыть в редакторе'],['stop','Скачать'],['key','Права и владелец'],['file','Переименовать'],['file','Копировать'],['trash','Удалить']].map(([i,t],k)=>`<button class="act ${k===5?'dng':''}">${ic(i)} ${t}</button>`).join('')});
M.sheet({id:'file-new',title:'Создать',under:'srv-files',flush:1,body:()=>['Новый файл','Новая папка','Загрузить с телефона'].map(t=>`<button class="act">${ic('plus')} ${t}</button>`).join('')});
M.sheet({id:'svc-act',title:'redis-server',under:'srv-services',flush:1,body:()=>[['play','Запустить'],['refresh','Перезапустить'],['stop','Остановить'],['bolt','Автозапуск: включён'],['logs','Логи службы'],['x','Игнорировать упавшую']].map(([i,t],k)=>`<button class="act ${k===2?'dng':''}">${ic(i)} ${t}</button>`).join('')});
M.sheet({id:'proc-act',title:'mysqld · PID 1021',under:'srv-processes',flush:1,body:()=>`<button class="act">${ic('stop')} Завершить (SIGTERM)</button><button class="act dng">${ic('x')} Убить (SIGKILL)</button>`});
M.sheet({id:'log-src',title:'Источник',under:'srv-logs',flush:1,body:()=>['Все источники','nginx','sshd','kernel','cron','redis'].map((t,i)=>`<button class="act">${t}${i?'':`<small>${ic('chev','s')}</small>`}</button>`).join('')});
M.sheet({id:'apt-progress',title:'Установка',under:'srv-updates',full:1,body:()=>`<div class="stack"><div class="pbar"><i style="width:58%"></i></div><div class="muted small">7 из 12 · openssl, libssl3 …</div><div class="list">${['openssl','libssl3','curl','nginx','tzdata'].map((p,i)=>`<div class="li"><div class="mn"><div class="l1"><b>${p}</b></div></div>${tag(i<3?'готово':i===3?'установка':'ожидание',i<3?'ok':i===3?'warn':'muted')}</div>`).join('')}</div></div>`,foot:`<button class="btn f1" data-close>Скрыть</button>`});
})();
