import { useQuery } from '@tanstack/react-query';
import { api } from './api';
import { Sidebar } from './components/Sidebar';
import { useRoute, href } from './router';
import { ChatRoom } from './pages/ChatRoom';
import { BotsPage } from './pages/BotsPage';
import { BotForm } from './pages/BotForm';
import { BotDetail } from './pages/BotDetail';
import { ChatCreate } from './pages/ChatCreate';
import { Onboard } from './pages/Onboard';
import { SettingsPage } from './pages/SettingsPage';

export function App() {
  const route = useRoute();
  return (
    <div className="app">
      <Sidebar route={route} />
      <div className="content">
        {route.page === 'chat' && <ChatRoom key={route.id} chatId={route.id} />}
        {route.page === 'bots' && <BotsPage key={route.scope} scope={route.scope} />}
        {route.page === 'bot' && <BotDetail key={route.handle} handle={route.handle} />}
        {route.page === 'newBot' && <BotForm key={route.edit ?? route.scope} scope={route.scope} edit={route.edit} />}
        {route.page === 'newChat' && <ChatCreate key={route.scope} scope={route.scope} />}
        {route.page === 'onboard' && <Onboard />}
        {route.page === 'settings' && <SettingsPage />}
        {route.page === 'home' && <Home />}
      </div>
    </div>
  );
}

function Home() {
  const projects = useQuery({ queryKey: ['projects'], queryFn: api.projects });
  return (
    <main className="page">
      <h1>Realm</h1>
      <p className="sub">Pick a chat or bots in the sidebar, or start with one of these.</p>
      {projects.isError && <p className="error">Cannot reach the Realm daemon: {String(projects.error)}</p>}
      <div className="grid3">
        <a className="card" href={href({ page: 'onboard' })}>
          <b>Add a project</b>
          <span className="sub">Register a folder, pick starter bots, open its first chat.</span>
        </a>
        <a className="card" href={href({ page: 'newBot', scope: 'global' })}>
          <b>Create a global bot</b>
          <span className="sub">Works in global chats and any project chat it joins.</span>
        </a>
        <a className="card" href={href({ page: 'newChat', scope: 'global' })}>
          <b>Start a global chat</b>
          <span className="sub">Talk to several global bots in one room.</span>
        </a>
      </div>
    </main>
  );
}
