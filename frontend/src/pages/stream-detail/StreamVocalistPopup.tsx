import { Link } from 'react-router-dom';

import type { StreamDetailModel } from './useStreamDetail';

export default function StreamVocalistPopup({ model }: { model: StreamDetailModel }) {
  const { setVocalistPopupSingers, vocalistPopupSingers } = model;
  if (!vocalistPopupSingers) return null;
  return (
    <div
      className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50"
      onClick={() => setVocalistPopupSingers(null)}
    >
      <div
        className="bg-white rounded-lg shadow-xl max-w-md w-full mx-4 p-6"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex justify-between items-center mb-4">
          <h3 className="text-lg font-bold text-gray-900">ボーカル一覧</h3>
          <button
            onClick={() => setVocalistPopupSingers(null)}
            className="text-gray-400 hover:text-gray-600 transition-colors"
          >
            <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
        <div className="space-y-2 max-h-96 overflow-y-auto">
          {vocalistPopupSingers.map((singer) => (
            <Link
              key={singer.id}
              to={`/channels/${singer.id}`}
              className="flex items-center gap-3 p-3 rounded-lg hover:bg-gray-50 transition-colors"
              onClick={() => setVocalistPopupSingers(null)}
            >
              <img
                src={
                  singer.photo_url ||
                  `https://holodex.net/statics/channelImg/${singer.id}/50.png`
                }
                alt={singer.name}
                className="w-12 h-12 rounded-full border-2 border-gray-200"
                onError={(e) => {
                  e.currentTarget.onerror = null;
                  e.currentTarget.src = `https://holodex.net/statics/channelImg/${singer.id}/50.png`;
                }}
              />
              <div className="flex-1">
                <div className="font-medium text-gray-900">{singer.name}</div>
                {singer.english_name && (
                  <div className="text-sm text-gray-500">{singer.english_name}</div>
                )}
              </div>
              <svg className="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
              </svg>
            </Link>
          ))}
        </div>
      </div>
    </div>
  );
}
