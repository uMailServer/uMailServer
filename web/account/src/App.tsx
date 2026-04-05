import { BrowserRouter, Routes, Route } from 'react-router-dom'
import Layout from './components/Layout'
import LoginPage from './pages/Login'
import ProfilePage from './pages/Profile'
import PasswordPage from './pages/Password'
import TwoFactorPage from './pages/TwoFactor'
import ForwardingPage from './pages/Forwarding'
import VacationPage from './pages/Vacation'
import FiltersPage from './pages/Filters'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<ProfilePage />} />
          <Route path="login" element={<LoginPage />} />
          <Route path="profile" element={<ProfilePage />} />
          <Route path="password" element={<PasswordPage />} />
          <Route path="2fa" element={<TwoFactorPage />} />
          <Route path="forwarding" element={<ForwardingPage />} />
          <Route path="vacation" element={<VacationPage />} />
          <Route path="filters" element={<FiltersPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

export default App
