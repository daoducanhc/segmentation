import axios from 'axios';
import type { Segment, SegmentDefinition } from './types';

const API_BASE = 'http://localhost:8000/v1';

const api = axios.create({
  baseURL: API_BASE,
  headers: { 'Content-Type': 'application/json' },
});

// Segments CRUD
export const createSegment = (segment: Segment) =>
  api.post('/segments', segment).then(r => r.data);

export const listSegments = (page = 1, pageSize = 20) =>
  api.get('/segments', { params: { page, pageSize } }).then(r => r.data);

export const getSegment = (id: string) =>
  api.get(`/segments/${id}`).then(r => r.data);

export const updateSegment = (id: string, segment: Partial<Segment>) =>
  api.put(`/segments/${id}`, segment).then(r => r.data);

export const deleteSegment = (id: string) =>
  api.delete(`/segments/${id}`).then(r => r.data);

export const evaluateSegment = (id: string, limit = 100, offset = 0) =>
  api.post(`/segments/${id}/evaluate`, { limit, offset }).then(r => r.data);

export const previewSegment = (definition: SegmentDefinition, limit = 100) =>
  api.post('/segments/preview', { definition, limit }).then(r => r.data);

// File upload for static segment
export const uploadStaticSegment = async (
  name: string,
  file: File,
  headerName: string
) => {
  const formData = new FormData();
  formData.append('name', name);
  formData.append('file', file);
  formData.append('header_name', headerName);
  
  return axios.post(`${API_BASE}/segments/upload-file`, formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  }).then(r => r.data);
};

// Add users to static segment
export const addUsersToSegment = async (
  segmentId: string,
  file: File,
  headerName: string
) => {
  const formData = new FormData();
  formData.append('file', file);
  formData.append('header_name', headerName);
  
  return axios.post(`${API_BASE}/segments/${segmentId}/users-file`, formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  }).then(r => r.data);
};

// Get distinct values for profile fields
export const getDistinctValues = (field: string) =>
  api.get(`/segments/distinct-values/${field}`).then(r => r.data);
