import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './styles.css';

// cos-js-sdk-v5 在浏览器环境依赖 global，补一个垫片
(window as unknown as { global: Window }).global = window;

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
