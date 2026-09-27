export namespace main {
	
	export class FileItem {
	    name: string;
	    size: number;
	    modified: string;
	    mtime: number;
	
	    static createFrom(source: any = {}) {
	        return new FileItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.modified = source["modified"];
	        this.mtime = source["mtime"];
	    }
	}
	export class ServerState {
	    running: boolean;
	    port: number;
	    portFree: boolean;
	    dataDir: string;
	    url: string;
	    localIP: string;
	    count: number;
	    users: number;
	    error: string;
	    configPath: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.port = source["port"];
	        this.portFree = source["portFree"];
	        this.dataDir = source["dataDir"];
	        this.url = source["url"];
	        this.localIP = source["localIP"];
	        this.count = source["count"];
	        this.users = source["users"];
	        this.error = source["error"];
	        this.configPath = source["configPath"];
	    }
	}
	export class TokenInfo {
	    id: string;
	    name: string;
	    enabled: boolean;
	    createdAt: string;
	    lastSeen: string;
	
	    static createFrom(source: any = {}) {
	        return new TokenInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.createdAt = source["createdAt"];
	        this.lastSeen = source["lastSeen"];
	    }
	}
	export class UserInfo {
	    id: string;
	    name: string;
	    enabled: boolean;
	    createdAt: string;
	    tokenCount: number;
	    fileCount: number;
	
	    static createFrom(source: any = {}) {
	        return new UserInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.createdAt = source["createdAt"];
	        this.tokenCount = source["tokenCount"];
	        this.fileCount = source["fileCount"];
	    }
	}

}

