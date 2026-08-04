-- UNO Server MySQL schema（utf8mb4 / UTC 语义由连接 DSN 保证）
-- 对应 internal/model/entity

CREATE TABLE IF NOT EXISTS players (
    id            BIGINT       NOT NULL COMMENT 'player_id',
    nickname      VARCHAR(32)  NOT NULL COMMENT '展示昵称，允许重名',
    created_at    DATETIME(3)  NOT NULL COMMENT '首次创建 UTC',
    updated_at    DATETIME(3)  NOT NULL COMMENT '资料更新 UTC',
    last_login_at DATETIME(3)  NOT NULL COMMENT '最近登录 UTC',
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='玩家';

CREATE TABLE IF NOT EXISTS matches (
    id                BIGINT       NOT NULL COMMENT 'match_id',
    room_id           VARCHAR(16)  NOT NULL COMMENT '业务房间号',
    status            VARCHAR(16)  NOT NULL COMMENT 'playing|finished|aborted',
    player_count      TINYINT      NOT NULL COMMENT '开局人数 2-6',
    winner_player_id  BIGINT       NULL COMMENT '胜者 player_id',
    started_at        DATETIME(3)  NOT NULL COMMENT '开局 UTC',
    ended_at          DATETIME(3)  NULL COMMENT '结束 UTC',
    created_at        DATETIME(3)  NOT NULL COMMENT '行创建 UTC',
    PRIMARY KEY (id),
    KEY idx_room_id (room_id),
    KEY idx_started_at (started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='对局记录';

CREATE TABLE IF NOT EXISTS match_results (
    id          BIGINT      NOT NULL COMMENT '代理主键',
    match_id    BIGINT      NOT NULL COMMENT '关联 matches.id',
    player_id   BIGINT      NOT NULL COMMENT '玩家',
    seat_index  TINYINT     NOT NULL COMMENT '座位 0-5',
    is_winner   TINYINT(1)  NOT NULL COMMENT '是否胜者',
    score       INT         NOT NULL COMMENT '本局得分',
    hand_points INT         NOT NULL COMMENT '结束时手牌点数',
    cards_left  TINYINT     NOT NULL COMMENT '剩余手牌张数',
    created_at  DATETIME(3) NOT NULL COMMENT '写入 UTC',
    PRIMARY KEY (id),
    UNIQUE KEY uk_match_player (match_id, player_id),
    KEY idx_player_id (player_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='对局结果';
