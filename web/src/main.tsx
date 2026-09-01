import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Shell } from './ui/Shell';
import './index.css';

const root = document.getElementById('root');
if (!root) throw new Error('no #root');
createRoot(root).render(<StrictMode><Shell /></StrictMode>);
