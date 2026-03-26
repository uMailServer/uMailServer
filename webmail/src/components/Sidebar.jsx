import { useEmail } from '../contexts/EmailContext'

function Sidebar() {
  const { folders, currentFolder, changeFolder, getUnreadCount } = useEmail()

  return (
    <aside className="sidebar">
      <ul className="folder-list">
        {folders.map(folder => {
          const isActive = folder === currentFolder
          const count = getUnreadCount(folder)

          return (
            <li
              key={folder}
              className={isActive ? 'active' : ''}
              onClick={() => changeFolder(folder)}
            >
              <span>{folder}</span>
              {count > 0 && <span className="badge">{count}</span>}
            </li>
          )
        })}
      </ul>
    </aside>
  )
}

export default Sidebar
