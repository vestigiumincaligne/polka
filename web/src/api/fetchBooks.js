import api from "./api";

const buildQuery = (params) => {
  const usp = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") continue;
    usp.set(key, String(value));
  }
  const qs = usp.toString();
  return qs ? `?${qs}` : "";
};

const get = (endpoint, params = {}) => api.getJson(`main/getBooks/${endpoint}${buildQuery(params)}`);

export const fetchConfig = () => get("getConfig");

export const fetchHomeShelves = ({ limit } = {}) => get("getHomeShelves", { limit });

export const fetchCatalogShelves = ({ limit, count } = {}) =>
  get("getCatalogShelves", { limit, count });

export const fetchShelfBooks = ({ shelfId, limit, offset } = {}) =>
  get("getShelfBooks", { shelfId, limit, offset });

export const fetchSearchStats = ({ search, selectedGroupID } = {}) => get("getSearchStats", { search, selectedGroupID });

export const fetchSearchTitles = ({ search, selectedGroupID } = {}) => get("getSearchTitles", { search, selectedGroupID });

export const fetchSearchAuthors = ({ search, selectedGroupID } = {}) => get("getSearchAuthors", { search, selectedGroupID });

export const fetchSearchSeries = ({ search, selectedGroupID } = {}) => get("getSearchSeries", { search, selectedGroupID });

export const fetchSearchGenres = ({ search } = {}) => get("getSearchGenres", { search });

export const fetchAuthorBooks = ({ selectedItemID, selectedGroupID } = {}) =>
  get("getSearchAuthorBooks", { selectedItemID, selectedGroupID });

export const fetchSeriesBooks = ({ selectedItemID, selectedGroupID } = {}) =>
  get("getSearchSeriesBooks", { selectedItemID, selectedGroupID });

export const fetchBookForm = ({ selectedItemID } = {}) => get("getBookForm", { selectedItemID });

export const fetchExternalEnrichment = ({ bookId, title, author, isbn } = {}) =>
  get("getExternalEnrichment", { bookId, title, author, isbn });

export const fetchSimilarBooks = ({ bookId, title, author } = {}) =>
  get("getSimilarBooks", { bookId, title, author });
