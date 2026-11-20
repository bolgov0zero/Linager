# Keys

"Keys" is your personal encrypted file of logins and passwords. It saves typing them: in the RDP form you choose a key and the login, the domain and the password are filled in. The item "Keys" is in the menu of the top bar of every user of the panel (an administrator, an operator, an auditor); everyone has their own file.

## The main points

- **The file is kept by you, not by the server.** The database of the panel, `/etc/linager` and the audit journal have neither the keys nor the passphrase. A leaked database or a compromised panel server does not reveal the keys.
- **Decrypted in the memory of the tab only.** The file is opened once per tab (choose the file and enter the passphrase); the decrypted data lives in the memory of the page and is saved nowhere: not in `localStorage`, not in `sessionStorage`, not in IndexedDB. Closing the tab, reloading the page, signing out and the auto-lock forget everything.
- **The password goes only to the RDP client** (in the browser). It is not sent to the panel server.
- **The passphrase cannot be recovered.** A forgotten passphrase means a lost file. Make a copy of the file and keep it apart.

## How to use it

1. Menu of the top bar → **Keys**.
2. **Create a vault:** a passphrase (at least 10 characters, several words are better) twice, "Create and download the file". The browser downloads `linager-keys.lvault`; keep it in a safe place.
3. **Open a file:** "Choose…", the `.lvault` file, the passphrase, "Open". Decrypting takes from one to a few seconds, on purpose.
4. The window then shows the table of the keys: **Add a key**, **Edit**, **Delete** (confirmed in the row), a search and a filter by type.
5. **Save the file** encrypts the vault again and downloads a new `.lvault`; replace the old file with it. While there are unsaved changes the button has the accent colour, a badge shows in the window, and the browser asks for confirmation when the tab is closed.
6. **Close the window** leaves the vault open until the tab is closed. **Close the vault** forgets the decrypted data at once (with unsaved changes it asks what to do).

The auto-lock (15 minutes of silence by default; 5, 30 or off; the choice is remembered in the browser) closes the vault by itself. With unsaved changes it stays open until you save the file, and closes then.

## A key

| Field | What it is |
|---|---|
| Name | Shown in the lists instead of the login and the password. |
| Use for | **RDP**, **UAC** or **Any**. The RDP form offers the keys RDP and Any. The type UAC is kept for typing into the UAC prompt and the Windows sign-in screen (in the Assistant); that part comes later. |
| Binding | To groups and servers of the panel (several of them): the servers are grouped by their groups, the switch of a group covers all its servers, the ones that appear later too. Nothing chosen: the key is for all servers. |
| Login, domain | The domain is optional. |
| Password | Can be shown, and a random one (20 characters) can be generated. |
| Note | Optional. |

**A binding only sets the order in the lists, it is not protection.** In the RDP form the keys bound to this server come first, then those of its group, then those for all; the rest are behind "Show all". Every key of the file is open to whoever opened the file. If the servers or groups a key is bound to are removed from the panel, the key gets the badge "not found", and a key with no living binding counts as general; the dead binding can be taken off in the window of the key.

## The RDP form

In the connection form (the tab "RDP" and the floating window) there is a list **"Key"** above the login. When the vault is closed, the list has the button "Open…" that opens the window "Keys". Choosing a key fills the login, the domain and the password; the connection then goes as usual, the password enters the CredSSP of the client and is sent nowhere. The connection is written to the audit with the login, as before.

## The file and the encryption

The format `.lvault`: a header (the mark `LGRK`, the version, the parameters, a salt, a nonce) and the encrypted content. The encryption key comes from the passphrase with **Argon2id** (64 MiB of memory, 3 passes, 1 lane; the parameters are written in the file), the content is encrypted with **AES-256-GCM**, and the header is authenticated: a changed byte anywhere in the file gives an error, not garbled data. The passphrase is brought to one Unicode form (NFKC), so the same word typed on another keyboard opens the file. The file is at most 1 MB and has at most 500 keys; a file with foreign parameters that ask for too much memory is not opened. The cryptography (`@noble/hashes`, `@noble/ciphers`) is plain JavaScript: it works over HTTP, where `crypto.subtle` is not available, and needs no WebAssembly.

## What is not included

Typing the login and the password into the UAC prompt and the Windows sign-in screen (a key button in the Assistant), syncing the file between devices (the file can be put in iCloud, Syncthing and the like; two copies edited at once are not merged), a shared team file with access control, history and attachments.
