import { Routes } from '@angular/router';
import { Folder } from './pages/folder';
import { Home } from './pages/home';

export const routes: Routes = [
  { path: '', component: Home, title: 'ferry' },
  { path: 'browse/:share', component: Folder, title: 'ferry' },
  { path: '**', redirectTo: '' },
];
